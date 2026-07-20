package auth

import (
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"gofr.dev/pkg/gofr"

	"github.com/opengittr/ogtr/backend/apierrors"
)

// DefaultGoogleJWKSURL is Google's published signing-key set. Tests and local
// E2E point GOOGLE_JWKS_URL at a local JWKS server instead; the verification
// logic is identical either way — there is no bypass path.
const DefaultGoogleJWKSURL = "https://www.googleapis.com/oauth2/v3/certs"

const (
	jwksFetchTimeout    = 10 * time.Second
	jwksRefetchCooldown = time.Minute
	jwksCacheTTL        = time.Hour
)

var errInvalidGoogleToken = apierrors.Unauthorized("invalid google id token")

// GoogleProvider verifies Google ID tokens: RS256 signature against the JWKS,
// issuer, audience (GOOGLE_CLIENT_ID) and expiry.
type GoogleProvider struct {
	clientID string
	jwksURL  string
	client   *http.Client

	mu        sync.Mutex
	keys      map[string]*rsa.PublicKey
	fetchedAt time.Time
}

// NewGoogleProvider builds a GoogleProvider. An empty jwksURL falls back to
// DefaultGoogleJWKSURL.
func NewGoogleProvider(clientID, jwksURL string) *GoogleProvider {
	if jwksURL == "" {
		jwksURL = DefaultGoogleJWKSURL
	}

	return &GoogleProvider{
		clientID: clientID,
		jwksURL:  jwksURL,
		client:   &http.Client{Timeout: jwksFetchTimeout},
		keys:     map[string]*rsa.PublicKey{},
	}
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

	_, err := jwt.ParseWithClaims(credential, claims, p.keyFunc,
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

// keyFunc resolves the token's kid against the (cached) JWKS.
func (p *GoogleProvider) keyFunc(token *jwt.Token) (any, error) {
	kid, _ := token.Header["kid"].(string)
	if kid == "" {
		return nil, fmt.Errorf("id token has no kid header")
	}

	key, err := p.key(kid)
	if err != nil {
		return nil, err
	}

	return key, nil
}

func (p *GoogleProvider) key(kid string) (*rsa.PublicKey, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if key, ok := p.keys[kid]; ok && time.Since(p.fetchedAt) < jwksCacheTTL {
		return key, nil
	}

	// Unknown or stale kid: refetch, but never hammer the JWKS endpoint.
	if time.Since(p.fetchedAt) > jwksRefetchCooldown || len(p.keys) == 0 {
		if err := p.fetchLocked(); err != nil {
			return nil, err
		}
	}

	key, ok := p.keys[kid]
	if !ok {
		return nil, fmt.Errorf("no JWKS key for kid %q", kid)
	}

	return key, nil
}

type jwksDoc struct {
	Keys []jwksKey `json:"keys"`
}

type jwksKey struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	N   string `json:"n"`
	E   string `json:"e"`
}

// fetchLocked refreshes the key cache; the caller must hold p.mu.
func (p *GoogleProvider) fetchLocked() error {
	resp, err := p.client.Get(p.jwksURL)
	if err != nil {
		return fmt.Errorf("fetching JWKS: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("fetching JWKS: unexpected status %d", resp.StatusCode)
	}

	var doc jwksDoc
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return fmt.Errorf("decoding JWKS: %w", err)
	}

	keys := make(map[string]*rsa.PublicKey, len(doc.Keys))

	for _, k := range doc.Keys {
		if !strings.EqualFold(k.Kty, "RSA") || k.Kid == "" {
			continue
		}

		pub, err := rsaKeyFromJWK(k)
		if err != nil {
			return fmt.Errorf("parsing JWKS key %q: %w", k.Kid, err)
		}

		keys[k.Kid] = pub
	}

	if len(keys) == 0 {
		return fmt.Errorf("JWKS at %s contains no usable RSA keys", p.jwksURL)
	}

	p.keys = keys
	p.fetchedAt = time.Now()

	return nil
}

func rsaKeyFromJWK(k jwksKey) (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(k.N)
	if err != nil {
		return nil, fmt.Errorf("modulus: %w", err)
	}

	eBytes, err := base64.RawURLEncoding.DecodeString(k.E)
	if err != nil {
		return nil, fmt.Errorf("exponent: %w", err)
	}

	e := new(big.Int).SetBytes(eBytes)
	if !e.IsInt64() || e.Int64() <= 0 || e.Int64() > int64(1)<<31 {
		return nil, fmt.Errorf("exponent out of range")
	}

	return &rsa.PublicKey{N: new(big.Int).SetBytes(nBytes), E: int(e.Int64())}, nil
}
