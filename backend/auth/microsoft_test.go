package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testWorkTenant = "72f988bf-86f1-41af-91ab-2d7cd011db47"
	// The fixed consumer tenant for personal Microsoft accounts.
	testConsumerTenant = "9188040d-6c67-4c5b-b112-36a304b66dad"
)

func msIssuer(tid string) string {
	return microsoftIssuerPrefix + tid + microsoftIssuerSuffix
}

func microsoftClaimsFor(email, name string) jwt.MapClaims {
	return jwt.MapClaims{
		"iss":   msIssuer(testWorkTenant),
		"tid":   testWorkTenant,
		"aud":   testClientID,
		"sub":   "ms-sub-1",
		"email": email,
		"name":  name,
		"iat":   time.Now().Unix(),
		"nbf":   time.Now().Unix(),
		"exp":   time.Now().Add(time.Hour).Unix(),
	}
}

//nolint:funlen // table test enumerating the issuer/audience/claims matrix
func TestMicrosoftProvider_Verify(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	otherKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	srv := newJWKS(t, &key.PublicKey, testKid)

	override := func(mutate func(c jwt.MapClaims)) jwt.MapClaims {
		c := microsoftClaimsFor("alice@example.com", "Alice")
		mutate(c)

		return c
	}

	tests := []struct {
		desc       string
		credential string
		wantErr    string
		want       Identity
	}{
		{
			desc:       "valid work-tenant token",
			credential: signRS256Token(t, key, testKid, microsoftClaimsFor("alice@example.com", "Alice")),
			want:       Identity{Email: "alice@example.com", Name: "Alice"},
		},
		{
			desc: "valid personal-account token (consumer tenant)",
			credential: signRS256Token(t, key, testKid, override(func(c jwt.MapClaims) {
				c["iss"] = msIssuer(testConsumerTenant)
				c["tid"] = testConsumerTenant
			})),
			want: Identity{Email: "alice@example.com", Name: "Alice"},
		},
		{
			desc: "issuer tenant does not match tid claim",
			credential: signRS256Token(t, key, testKid, override(func(c jwt.MapClaims) {
				c["iss"] = msIssuer("11111111-2222-3333-4444-555555555555")
			})),
			wantErr: errInvalidMicrosoftToken.Error(),
		},
		{
			desc: "wrong issuer host",
			credential: signRS256Token(t, key, testKid, override(func(c jwt.MapClaims) {
				c["iss"] = "https://evil.example/" + testWorkTenant + "/v2.0"
			})),
			wantErr: errInvalidMicrosoftToken.Error(),
		},
		{
			desc: "host must be exact, not a prefix of another domain",
			credential: signRS256Token(t, key, testKid, override(func(c jwt.MapClaims) {
				c["iss"] = "https://login.microsoftonline.com.evil.example/" + testWorkTenant + "/v2.0"
			})),
			wantErr: errInvalidMicrosoftToken.Error(),
		},
		{
			desc: "v1.0-style issuer (sts.windows.net) rejected",
			credential: signRS256Token(t, key, testKid, override(func(c jwt.MapClaims) {
				c["iss"] = "https://sts.windows.net/" + testWorkTenant + "/"
			})),
			wantErr: errInvalidMicrosoftToken.Error(),
		},
		{
			desc: "issuer missing the /v2.0 suffix",
			credential: signRS256Token(t, key, testKid, override(func(c jwt.MapClaims) {
				c["iss"] = microsoftIssuerPrefix + testWorkTenant
			})),
			wantErr: errInvalidMicrosoftToken.Error(),
		},
		{
			desc: "issuer with empty tenant segment",
			credential: signRS256Token(t, key, testKid, override(func(c jwt.MapClaims) {
				c["iss"] = "https://login.microsoftonline.com//v2.0"
				c["tid"] = ""
			})),
			wantErr: errInvalidMicrosoftToken.Error(),
		},
		{
			desc: "missing tid claim",
			credential: signRS256Token(t, key, testKid, override(func(c jwt.MapClaims) {
				delete(c, "tid")
			})),
			wantErr: errInvalidMicrosoftToken.Error(),
		},
		{
			desc:       "wrong audience",
			credential: signRS256Token(t, key, testKid, override(func(c jwt.MapClaims) { c["aud"] = "someone-else" })),
			wantErr:    errInvalidMicrosoftToken.Error(),
		},
		{
			desc: "expired",
			credential: signRS256Token(t, key, testKid, override(func(c jwt.MapClaims) {
				c["exp"] = time.Now().Add(-time.Hour).Unix()
			})),
			wantErr: errInvalidMicrosoftToken.Error(),
		},
		{
			desc: "not yet valid (nbf in the future)",
			credential: signRS256Token(t, key, testKid, override(func(c jwt.MapClaims) {
				c["nbf"] = time.Now().Add(time.Hour).Unix()
			})),
			wantErr: errInvalidMicrosoftToken.Error(),
		},
		{
			desc: "missing exp",
			credential: signRS256Token(t, key, testKid, override(func(c jwt.MapClaims) {
				delete(c, "exp")
			})),
			wantErr: errInvalidMicrosoftToken.Error(),
		},
		{
			desc: "email absent, email-shaped preferred_username is accepted",
			credential: signRS256Token(t, key, testKid, override(func(c jwt.MapClaims) {
				delete(c, "email")
				c["preferred_username"] = "bob@contoso.com"
			})),
			want: Identity{Email: "bob@contoso.com", Name: "Alice"},
		},
		{
			desc: "email claim wins over preferred_username",
			credential: signRS256Token(t, key, testKid, override(func(c jwt.MapClaims) {
				c["preferred_username"] = "other@contoso.com"
			})),
			want: Identity{Email: "alice@example.com", Name: "Alice"},
		},
		{
			desc: "email absent, non-email preferred_username rejected",
			credential: signRS256Token(t, key, testKid, override(func(c jwt.MapClaims) {
				delete(c, "email")
				c["preferred_username"] = "+1 425 555 0100"
			})),
			wantErr: errInvalidMicrosoftToken.Error(),
		},
		{
			desc: "email absent, display-name-decorated preferred_username rejected",
			credential: signRS256Token(t, key, testKid, override(func(c jwt.MapClaims) {
				delete(c, "email")
				c["preferred_username"] = "Bob <bob@contoso.com>"
			})),
			wantErr: errInvalidMicrosoftToken.Error(),
		},
		{
			desc: "no email and no preferred_username",
			credential: signRS256Token(t, key, testKid, override(func(c jwt.MapClaims) {
				delete(c, "email")
			})),
			wantErr: errInvalidMicrosoftToken.Error(),
		},
		{
			desc:       "unknown kid",
			credential: signRS256Token(t, key, "unknown-kid", microsoftClaimsFor("alice@example.com", "Alice")),
			wantErr:    errInvalidMicrosoftToken.Error(),
		},
		{
			desc:       "signed by a different key",
			credential: signRS256Token(t, otherKey, testKid, microsoftClaimsFor("alice@example.com", "Alice")),
			wantErr:    errInvalidMicrosoftToken.Error(),
		},
		{
			desc:       "garbage credential",
			credential: "definitely.not.a-jwt",
			wantErr:    errInvalidMicrosoftToken.Error(),
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			provider := NewMicrosoftProvider(testClientID, srv.URL)

			identity, err := provider.Verify(testCtx(t), tc.credential)

			if tc.wantErr != "" {
				require.Error(t, err)
				assert.Equal(t, tc.wantErr, err.Error())

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.want, identity)
		})
	}
}

func TestMicrosoftProvider_HS256Rejected(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	srv := newJWKS(t, &key.PublicKey, testKid)
	provider := NewMicrosoftProvider(testClientID, srv.URL)

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, microsoftClaimsFor("alice@example.com", "Alice"))
	token.Header["kid"] = testKid

	signed, err := token.SignedString([]byte("hmac-key"))
	require.NoError(t, err)

	_, err = provider.Verify(testCtx(t), signed)
	assert.Equal(t, errInvalidMicrosoftToken, err)
}

func TestMicrosoftProvider_UnconfiguredClientID(t *testing.T) {
	provider := NewMicrosoftProvider("", "")

	_, err := provider.Verify(testCtx(t), "anything")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not configured")
}

func TestMicrosoftProvider_JWKSUnavailable(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	provider := NewMicrosoftProvider(testClientID, srv.URL)

	_, err = provider.Verify(testCtx(t), signRS256Token(t, key, testKid, microsoftClaimsFor("a@b.co", "A")))
	assert.Equal(t, errInvalidMicrosoftToken, err)
}

func TestNewMicrosoftProvider_DefaultJWKSURL(t *testing.T) {
	provider := NewMicrosoftProvider(testClientID, "")
	assert.Equal(t, DefaultMicrosoftJWKSURL, provider.jwks.url)
}

// TestMicrosoftProvider_KidRotation exercises the shared JWKS cache through
// the provider: a token signed with a kid that appears only after the initial
// fetch verifies once the refetch cooldown has elapsed, and within the
// cooldown an unknown kid fails without hammering the endpoint.
func TestMicrosoftProvider_KidRotation(t *testing.T) {
	oldKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	newKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	var (
		fetches atomic.Int64
		serve   atomic.Pointer[rsa.PublicKey]
		kid     atomic.Pointer[string]
	)

	oldKid, newKid := "rotation-old", "rotation-new"

	serve.Store(&oldKey.PublicKey)
	kid.Store(&oldKid)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fetches.Add(1)

		pub := serve.Load()
		doc := map[string]any{"keys": []map[string]string{{
			"kty": "RSA",
			"kid": *kid.Load(),
			"n":   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
			"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
		}}}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(doc)
	}))
	t.Cleanup(srv.Close)

	provider := NewMicrosoftProvider(testClientID, srv.URL)
	claims := microsoftClaimsFor("alice@example.com", "Alice")

	// 1. Initial fetch caches the old key.
	_, err = provider.Verify(testCtx(t), signRS256Token(t, oldKey, oldKid, claims))
	require.NoError(t, err)
	assert.EqualValues(t, 1, fetches.Load())

	// 2. Provider rotates its keys.
	serve.Store(&newKey.PublicKey)
	kid.Store(&newKid)

	// Within the refetch cooldown the unknown kid fails WITHOUT a refetch.
	_, err = provider.Verify(testCtx(t), signRS256Token(t, newKey, newKid, claims))
	assert.Equal(t, errInvalidMicrosoftToken, err)
	assert.EqualValues(t, 1, fetches.Load(), "unknown kid within the cooldown must not refetch")

	// 3. Past the cooldown the unknown kid triggers a refetch and verifies.
	provider.jwks.mu.Lock()
	provider.jwks.fetchedAt = time.Now().Add(-2 * jwksRefetchCooldown)
	provider.jwks.mu.Unlock()

	_, err = provider.Verify(testCtx(t), signRS256Token(t, newKey, newKid, claims))
	require.NoError(t, err)
	assert.EqualValues(t, 2, fetches.Load())
}
