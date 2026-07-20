package auth

import (
	"github.com/golang-jwt/jwt/v5"
	"gofr.dev/pkg/gofr"

	"github.com/opengittr/ogtr/backend/apierrors"
)

// DefaultGoogleJWKSURL is Google's published signing-key set. Tests and local
// E2E point GOOGLE_JWKS_URL at a local JWKS server instead; the verification
// logic is identical either way — there is no bypass path.
const DefaultGoogleJWKSURL = "https://www.googleapis.com/oauth2/v3/certs"

var errInvalidGoogleToken = apierrors.Unauthorized("invalid google id token")

// GoogleProvider verifies Google ID tokens: RS256 signature against the JWKS,
// issuer, audience (GOOGLE_CLIENT_ID) and expiry.
type GoogleProvider struct {
	clientID string
	jwks     *jwksCache
}

// NewGoogleProvider builds a GoogleProvider. An empty jwksURL falls back to
// DefaultGoogleJWKSURL.
func NewGoogleProvider(clientID, jwksURL string) *GoogleProvider {
	if jwksURL == "" {
		jwksURL = DefaultGoogleJWKSURL
	}

	return &GoogleProvider{clientID: clientID, jwks: newJWKSCache(jwksURL)}
}

type googleClaims struct {
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	Name          string `json:"name"`
	jwt.RegisteredClaims
}

// Verify implements IdentityProvider for Google ID tokens.
func (p *GoogleProvider) Verify(ctx *gofr.Context, credential string) (Identity, error) {
	if p.clientID == "" {
		return Identity{}, apierrors.Unauthorized("google sign-in is not configured (GOOGLE_CLIENT_ID is empty)")
	}

	claims := &googleClaims{}

	_, err := jwt.ParseWithClaims(credential, claims, p.jwks.keyFunc,
		jwt.WithValidMethods([]string{jwt.SigningMethodRS256.Alg()}),
		jwt.WithAudience(p.clientID),
		jwt.WithExpirationRequired(),
	)
	if err != nil {
		ctx.Logger.Debugf("google id token rejected: %v", err)

		return Identity{}, errInvalidGoogleToken
	}

	if claims.Issuer != "https://accounts.google.com" && claims.Issuer != "accounts.google.com" {
		ctx.Logger.Debugf("google id token rejected: unexpected issuer %q", claims.Issuer)

		return Identity{}, errInvalidGoogleToken
	}

	if claims.Email == "" || !claims.EmailVerified {
		ctx.Logger.Debugf("google id token rejected: email missing or unverified")

		return Identity{}, errInvalidGoogleToken
	}

	return Identity{Email: claims.Email, Name: claims.Name}, nil
}
