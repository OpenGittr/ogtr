package auth

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gofr.dev/pkg/gofr"
	"gofr.dev/pkg/gofr/container"
)

const (
	testClientID = "test-client-id"
	testKid      = "test-kid-1"
)

func testCtx(t *testing.T) *gofr.Context {
	t.Helper()

	mockContainer, _ := container.NewMockContainer(t)

	return &gofr.Context{Context: context.Background(), Container: mockContainer}
}

// newJWKS starts an httptest JWKS server publishing the given key.
func newJWKS(t *testing.T, pub *rsa.PublicKey, kid string) *httptest.Server {
	t.Helper()

	doc := map[string]any{"keys": []map[string]string{{
		"kty": "RSA",
		"kid": kid,
		"n":   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
		"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
	}}}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(doc)
	}))
	t.Cleanup(srv.Close)

	return srv
}

func signGoogleToken(t *testing.T, key *rsa.PrivateKey, kid string, claims jwt.MapClaims) string {
	t.Helper()

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = kid

	signed, err := token.SignedString(key)
	require.NoError(t, err)

	return signed
}

func googleClaimsFor(email, name string) jwt.MapClaims {
	return jwt.MapClaims{
		"iss":            "https://accounts.google.com",
		"aud":            testClientID,
		"sub":            "google-sub-1",
		"email":          email,
		"email_verified": true,
		"name":           name,
		"iat":            time.Now().Unix(),
		"exp":            time.Now().Add(time.Hour).Unix(),
	}
}

func TestGoogleProvider_Verify(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	otherKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	srv := newJWKS(t, &key.PublicKey, testKid)

	override := func(mutate func(c jwt.MapClaims)) jwt.MapClaims {
		c := googleClaimsFor("alice@example.com", "Alice")
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
			desc:       "valid token",
			credential: signGoogleToken(t, key, testKid, googleClaimsFor("alice@example.com", "Alice")),
			want:       Identity{Email: "alice@example.com", Name: "Alice"},
		},
		{
			desc:       "wrong audience",
			credential: signGoogleToken(t, key, testKid, override(func(c jwt.MapClaims) { c["aud"] = "someone-else" })),
			wantErr:    errInvalidGoogleToken.Error(),
		},
		{
			desc:       "wrong issuer",
			credential: signGoogleToken(t, key, testKid, override(func(c jwt.MapClaims) { c["iss"] = "https://evil.example" })),
			wantErr:    errInvalidGoogleToken.Error(),
		},
		{
			desc: "expired",
			credential: signGoogleToken(t, key, testKid, override(func(c jwt.MapClaims) {
				c["exp"] = time.Now().Add(-time.Hour).Unix()
			})),
			wantErr: errInvalidGoogleToken.Error(),
		},
		{
			desc:       "email not verified",
			credential: signGoogleToken(t, key, testKid, override(func(c jwt.MapClaims) { c["email_verified"] = false })),
			wantErr:    errInvalidGoogleToken.Error(),
		},
		{
			desc:       "missing email",
			credential: signGoogleToken(t, key, testKid, override(func(c jwt.MapClaims) { delete(c, "email") })),
			wantErr:    errInvalidGoogleToken.Error(),
		},
		{
			desc:       "unknown kid",
			credential: signGoogleToken(t, key, "unknown-kid", googleClaimsFor("alice@example.com", "Alice")),
			wantErr:    errInvalidGoogleToken.Error(),
		},
		{
			desc:       "signed by a different key",
			credential: signGoogleToken(t, otherKey, testKid, googleClaimsFor("alice@example.com", "Alice")),
			wantErr:    errInvalidGoogleToken.Error(),
		},
		{
			desc:       "garbage credential",
			credential: "definitely.not.a-jwt",
			wantErr:    errInvalidGoogleToken.Error(),
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			provider := NewGoogleProvider(testClientID, srv.URL)

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

func TestGoogleProvider_HS256Rejected(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	srv := newJWKS(t, &key.PublicKey, testKid)
	provider := NewGoogleProvider(testClientID, srv.URL)

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, googleClaimsFor("alice@example.com", "Alice"))
	token.Header["kid"] = testKid

	signed, err := token.SignedString([]byte("hmac-key"))
	require.NoError(t, err)

	_, err = provider.Verify(testCtx(t), signed)
	assert.Equal(t, errInvalidGoogleToken, err)
}

func TestGoogleProvider_UnconfiguredClientID(t *testing.T) {
	provider := NewGoogleProvider("", "")

	_, err := provider.Verify(testCtx(t), "anything")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not configured")
}

func TestGoogleProvider_JWKSUnavailable(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	provider := NewGoogleProvider(testClientID, srv.URL)

	_, err = provider.Verify(testCtx(t), signGoogleToken(t, key, testKid, googleClaimsFor("a@b.co", "A")))
	assert.Equal(t, errInvalidGoogleToken, err)
}

func TestNewGoogleProvider_DefaultJWKSURL(t *testing.T) {
	provider := NewGoogleProvider(testClientID, "")
	assert.Equal(t, DefaultGoogleJWKSURL, provider.jwksURL)
}
