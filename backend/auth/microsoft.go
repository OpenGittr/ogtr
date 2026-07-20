package auth

import (
	"net/mail"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"gofr.dev/pkg/gofr"

	"github.com/opengittr/ogtr/backend/apierrors"
)

// DefaultMicrosoftJWKSURL is the Microsoft identity platform's published
// signing-key set for the multi-tenant "common" endpoint (v2.0). Tests and
// local E2E point MICROSOFT_JWKS_URL at a local JWKS server instead; the
// verification logic is identical either way — there is no bypass path.
const DefaultMicrosoftJWKSURL = "https://login.microsoftonline.com/common/discovery/v2.0/keys"

// The v2.0 issuer for a token from the "common" endpoint is per-tenant:
// https://login.microsoftonline.com/{tid}/v2.0, where {tid} is the token's
// own tid claim. Personal Microsoft accounts use the fixed consumer tenant
// 9188040d-6c67-4c5b-b112-36a304b66dad and need no special-casing — their
// issuer follows the same pattern.
const (
	microsoftIssuerPrefix = "https://login.microsoftonline.com/"
	microsoftIssuerSuffix = "/v2.0"
)

var errInvalidMicrosoftToken = apierrors.Unauthorized("invalid microsoft id token")

// MicrosoftProvider verifies Microsoft identity platform (v2.0) ID tokens:
// RS256 signature against the JWKS, per-tenant issuer pattern, audience
// (MICROSOFT_CLIENT_ID), expiry and not-before.
type MicrosoftProvider struct {
	clientID string
	jwks     *jwksCache
}

// NewMicrosoftProvider builds a MicrosoftProvider. An empty jwksURL falls
// back to DefaultMicrosoftJWKSURL.
func NewMicrosoftProvider(clientID, jwksURL string) *MicrosoftProvider {
	if jwksURL == "" {
		jwksURL = DefaultMicrosoftJWKSURL
	}

	return &MicrosoftProvider{clientID: clientID, jwks: newJWKSCache(jwksURL)}
}

type microsoftClaims struct {
	Email string `json:"email"`
	// PreferredUsername is the account's sign-in identifier. It is usually
	// the email address but is NOT guaranteed to be one (it can be a phone
	// number or an unrouteable UPN), so it is only used as an email fallback
	// when it parses as a bare RFC 5322 address.
	PreferredUsername string `json:"preferred_username"`
	Name              string `json:"name"`
	TenantID          string `json:"tid"`
	jwt.RegisteredClaims
}

// Verify implements IdentityProvider for Microsoft ID tokens.
func (p *MicrosoftProvider) Verify(ctx *gofr.Context, credential string) (Identity, error) {
	if p.clientID == "" {
		return Identity{}, apierrors.Unauthorized("microsoft sign-in is not configured (MICROSOFT_CLIENT_ID is empty)")
	}

	claims := &microsoftClaims{}

	// exp is required and exp/nbf/iat are validated by ParseWithClaims when
	// present; signature keys come from the JWKS by kid.
	_, err := jwt.ParseWithClaims(credential, claims, p.jwks.keyFunc,
		jwt.WithValidMethods([]string{jwt.SigningMethodRS256.Alg()}),
		jwt.WithAudience(p.clientID),
		jwt.WithExpirationRequired(),
	)
	if err != nil {
		ctx.Logger.Debugf("microsoft id token rejected: %v", err)

		return Identity{}, errInvalidMicrosoftToken
	}

	if !microsoftIssuerValid(claims.Issuer, claims.TenantID) {
		ctx.Logger.Debugf("microsoft id token rejected: issuer %q does not match tid %q", claims.Issuer, claims.TenantID)

		return Identity{}, errInvalidMicrosoftToken
	}

	email := microsoftEmail(claims)
	if email == "" {
		ctx.Logger.Debugf("microsoft id token rejected: no usable email claim")

		return Identity{}, errInvalidMicrosoftToken
	}

	return Identity{Email: email, Name: claims.Name}, nil
}

// microsoftIssuerValid checks the "common"-endpoint per-tenant issuer pattern
// structurally: the issuer must be exactly
// https://login.microsoftonline.com/{tid}/v2.0 with {tid} equal to the
// token's own tid claim. The tid itself is proven by the signature (it is a
// claim in the signed token); this check pins the issuer to the Microsoft
// identity platform and rules out cross-scheme/host/version confusion.
func microsoftIssuerValid(issuer, tid string) bool {
	if tid == "" {
		return false
	}

	if len(issuer) < len(microsoftIssuerPrefix)+len(microsoftIssuerSuffix) {
		return false
	}

	if !strings.HasPrefix(issuer, microsoftIssuerPrefix) || !strings.HasSuffix(issuer, microsoftIssuerSuffix) {
		return false
	}

	return issuer[len(microsoftIssuerPrefix):len(issuer)-len(microsoftIssuerSuffix)] == tid
}

// microsoftEmail picks the identity email: the email claim when present,
// falling back to preferred_username ONLY when it parses as a bare RFC 5322
// address (it can legally be a phone number or non-routeable UPN, which must
// never become an ogtr account email). Empty when neither qualifies.
func microsoftEmail(claims *microsoftClaims) string {
	if claims.Email != "" {
		return claims.Email
	}

	candidate := claims.PreferredUsername

	addr, err := mail.ParseAddress(candidate)
	if err != nil || addr.Address != candidate {
		return ""
	}

	return candidate
}
