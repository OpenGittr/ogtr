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
)

const (
	jwksFetchTimeout    = 10 * time.Second
	jwksRefetchCooldown = time.Minute
	jwksCacheTTL        = time.Hour
)

// jwksCache is the shared kid→RSA-public-key cache behind the OIDC providers
// (Google, Microsoft): keys are fetched from the provider's JWKS URL, cached
// for jwksCacheTTL, refetched on an unknown kid (key rotation) but never more
// often than jwksRefetchCooldown so a flood of bad tokens cannot hammer the
// endpoint.
type jwksCache struct {
	url    string
	client *http.Client

	mu        sync.Mutex
	keys      map[string]*rsa.PublicKey
	fetchedAt time.Time
}

func newJWKSCache(url string) *jwksCache {
	return &jwksCache{
		url:    url,
		client: &http.Client{Timeout: jwksFetchTimeout},
		keys:   map[string]*rsa.PublicKey{},
	}
}

// keyFunc resolves the token's kid against the (cached) JWKS; it is the
// jwt.Keyfunc both providers hand to ParseWithClaims.
func (c *jwksCache) keyFunc(token *jwt.Token) (any, error) {
	kid, _ := token.Header["kid"].(string)
	if kid == "" {
		return nil, fmt.Errorf("id token has no kid header")
	}

	key, err := c.key(kid)
	if err != nil {
		return nil, err
	}

	return key, nil
}

func (c *jwksCache) key(kid string) (*rsa.PublicKey, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if key, ok := c.keys[kid]; ok && time.Since(c.fetchedAt) < jwksCacheTTL {
		return key, nil
	}

	// Unknown or stale kid: refetch, but never hammer the JWKS endpoint.
	if time.Since(c.fetchedAt) > jwksRefetchCooldown || len(c.keys) == 0 {
		if err := c.fetchLocked(); err != nil {
			return nil, err
		}
	}

	key, ok := c.keys[kid]
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

// fetchLocked refreshes the key cache; the caller must hold c.mu.
func (c *jwksCache) fetchLocked() error {
	resp, err := c.client.Get(c.url)
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
		return fmt.Errorf("JWKS at %s contains no usable RSA keys", c.url)
	}

	c.keys = keys
	c.fetchedAt = time.Now()

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
