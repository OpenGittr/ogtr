package auth

import (
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/opengittr/ogtr/backend/apierrors"
	"github.com/opengittr/ogtr/backend/models"
)

// Token types carried in the "typ" claim so an access token can never be
// replayed as a refresh token or vice versa.
const (
	TokenTypeAccess  = "access"
	TokenTypeRefresh = "refresh"
)

// Default token lifetimes (ARCHITECTURE.md §4); overridable via
// ACCESS_TOKEN_TTL / REFRESH_TOKEN_TTL.
const (
	DefaultAccessTTL  = 15 * time.Minute
	DefaultRefreshTTL = 30 * 24 * time.Hour
)

const tokenIssuer = "ogtr"

var errInvalidSessionToken = apierrors.Unauthorized("invalid or expired token")

// SessionClaims are the claims inside ogtr's own HS256 session JWTs.
// OrgID 0 means "no active org" — a valid state for users without membership.
type SessionClaims struct {
	UserID    int64  `json:"user_id"`
	OrgID     int64  `json:"org_id"`
	Role      string `json:"role,omitempty"`
	TokenType string `json:"typ"`
	jwt.RegisteredClaims
}

// TokenIssuer signs and parses ogtr session tokens (stateless HS256
// JWTs, both access and refresh — see ARCHITECTURE.md §4 for the rationale).
type TokenIssuer struct {
	key        []byte
	accessTTL  time.Duration
	refreshTTL time.Duration
	now        func() time.Time
}

// NewTokenIssuer builds a TokenIssuer. Non-positive TTLs fall back to the
// defaults.
func NewTokenIssuer(signingKey string, accessTTL, refreshTTL time.Duration) *TokenIssuer {
	if accessTTL <= 0 {
		accessTTL = DefaultAccessTTL
	}

	if refreshTTL <= 0 {
		refreshTTL = DefaultRefreshTTL
	}

	return &TokenIssuer{key: []byte(signingKey), accessTTL: accessTTL, refreshTTL: refreshTTL, now: time.Now}
}

// IssuePair issues an access + refresh token scoped to the given user and org
// (orgID 0 and empty role for org-less users).
func (t *TokenIssuer) IssuePair(userID, orgID int64, role string) (models.TokenPair, error) {
	access, err := t.issue(TokenTypeAccess, t.accessTTL, userID, orgID, role)
	if err != nil {
		return models.TokenPair{}, err
	}

	refresh, err := t.issue(TokenTypeRefresh, t.refreshTTL, userID, orgID, role)
	if err != nil {
		return models.TokenPair{}, err
	}

	return models.TokenPair{AccessToken: access, RefreshToken: refresh}, nil
}

func (t *TokenIssuer) issue(typ string, ttl time.Duration, userID, orgID int64, role string) (string, error) {
	now := t.now()

	claims := SessionClaims{
		UserID:    userID,
		OrgID:     orgID,
		Role:      role,
		TokenType: typ,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    tokenIssuer,
			Subject:   strconv.FormatInt(userID, 10),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
		},
	}

	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(t.key)
}

// Parse validates a session token's signature, expiry, issuer and type, and
// returns its claims. All failures map to a 401.
func (t *TokenIssuer) Parse(raw, wantType string) (*SessionClaims, error) {
	claims := &SessionClaims{}

	_, err := jwt.ParseWithClaims(raw, claims,
		func(*jwt.Token) (any, error) { return t.key, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(tokenIssuer),
		jwt.WithExpirationRequired(),
		jwt.WithTimeFunc(t.now),
	)
	if err != nil {
		return nil, errInvalidSessionToken
	}

	if claims.TokenType != wantType || claims.UserID <= 0 {
		return nil, errInvalidSessionToken
	}

	return claims, nil
}
