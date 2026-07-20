package auth

import (
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testIssuer() *TokenIssuer {
	return NewTokenIssuer("test-signing-key", DefaultAccessTTL, DefaultRefreshTTL)
}

func TestTokenIssuer_IssueAndParse(t *testing.T) {
	tests := []struct {
		desc   string
		userID int64
		orgID  int64
		role   string
	}{
		{desc: "org-scoped owner", userID: 7, orgID: 3, role: "OWNER"},
		{desc: "org-scoped member", userID: 8, orgID: 4, role: "MEMBER"},
		{desc: "org-less user", userID: 9, orgID: 0, role: ""},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			issuer := testIssuer()

			pair, err := issuer.IssuePair(tc.userID, tc.orgID, tc.role)
			require.NoError(t, err)
			require.NotEmpty(t, pair.AccessToken)
			require.NotEmpty(t, pair.RefreshToken)

			access, err := issuer.Parse(pair.AccessToken, TokenTypeAccess)
			require.NoError(t, err)
			assert.Equal(t, tc.userID, access.UserID)
			assert.Equal(t, tc.orgID, access.OrgID)
			assert.Equal(t, tc.role, access.Role)
			assert.Equal(t, TokenTypeAccess, access.TokenType)

			refresh, err := issuer.Parse(pair.RefreshToken, TokenTypeRefresh)
			require.NoError(t, err)
			assert.Equal(t, tc.userID, refresh.UserID)
			assert.Equal(t, tc.orgID, refresh.OrgID)
		})
	}
}

func TestTokenIssuer_ParseRejections(t *testing.T) {
	issuer := testIssuer()

	pair, err := issuer.IssuePair(1, 2, "MEMBER")
	require.NoError(t, err)

	expiredIssuer := testIssuer()
	expiredIssuer.now = func() time.Time { return time.Now().Add(-2 * DefaultRefreshTTL) }

	expiredPair, err := expiredIssuer.IssuePair(1, 2, "MEMBER")
	require.NoError(t, err)

	otherKey := NewTokenIssuer("some-other-key", DefaultAccessTTL, DefaultRefreshTTL)

	otherPair, err := otherKey.IssuePair(1, 2, "MEMBER")
	require.NoError(t, err)

	noneToken, err := jwt.NewWithClaims(jwt.SigningMethodNone, SessionClaims{
		UserID: 1, TokenType: TokenTypeAccess,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    tokenIssuer,
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		},
	}).SignedString(jwt.UnsafeAllowNoneSignatureType)
	require.NoError(t, err)

	tests := []struct {
		desc     string
		raw      string
		wantType string
	}{
		{desc: "garbage token", raw: "not-a-jwt", wantType: TokenTypeAccess},
		{desc: "empty token", raw: "", wantType: TokenTypeAccess},
		{desc: "refresh presented as access", raw: pair.RefreshToken, wantType: TokenTypeAccess},
		{desc: "access presented as refresh", raw: pair.AccessToken, wantType: TokenTypeRefresh},
		{desc: "expired token", raw: expiredPair.AccessToken, wantType: TokenTypeAccess},
		{desc: "wrong signing key", raw: otherPair.AccessToken, wantType: TokenTypeAccess},
		{desc: "alg none", raw: noneToken, wantType: TokenTypeAccess},
		{desc: "tampered payload", raw: tamper(pair.AccessToken), wantType: TokenTypeAccess},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			claims, err := issuer.Parse(tc.raw, tc.wantType)
			assert.Nil(t, claims)
			assert.Equal(t, errInvalidSessionToken, err)
		})
	}
}

// tamper flips a character in the JWT payload segment.
func tamper(token string) string {
	parts := strings.SplitN(token, ".", 3)
	payload := []byte(parts[1])
	if payload[0] == 'A' {
		payload[0] = 'B'
	} else {
		payload[0] = 'A'
	}

	return parts[0] + "." + string(payload) + "." + parts[2]
}

func TestNewTokenIssuer_TTLDefaults(t *testing.T) {
	issuer := NewTokenIssuer("k", 0, -1)
	assert.Equal(t, DefaultAccessTTL, issuer.accessTTL)
	assert.Equal(t, DefaultRefreshTTL, issuer.refreshTTL)
}
