package auth

import (
	"context"

	"github.com/opengittr/ogtr/backend/apierrors"
)

type claimsContextKey struct{}

// ContextWithClaims returns a context carrying validated session claims; the
// middleware calls this after token verification.
func ContextWithClaims(ctx context.Context, claims *SessionClaims) context.Context {
	return context.WithValue(ctx, claimsContextKey{}, claims)
}

// ClaimsFromContext returns the session claims stored by the middleware. It
// errors (401) when called on a request that never passed authentication —
// which would mean a route was wired outside the middleware by mistake.
func ClaimsFromContext(ctx context.Context) (*SessionClaims, error) {
	claims, ok := ctx.Value(claimsContextKey{}).(*SessionClaims)
	if !ok || claims == nil {
		return nil, apierrors.Unauthorized("authentication required")
	}

	return claims, nil
}

type apiKeyContextKey struct{}

// ContextWithAPIKey returns a context carrying the raw X-API-Key header
// value; the middleware calls this on the two key-auth routes instead of JWT
// validation. The key is NOT yet authenticated — the handler must resolve it
// via the API-key service (the hash lookup needs the database, which the
// middleware does not have).
func ContextWithAPIKey(ctx context.Context, rawKey string) context.Context {
	return context.WithValue(ctx, apiKeyContextKey{}, rawKey)
}

// APIKeyFromContext returns the raw API key forwarded by the middleware, and
// whether one was present.
func APIKeyFromContext(ctx context.Context) (string, bool) {
	rawKey, ok := ctx.Value(apiKeyContextKey{}).(string)

	return rawKey, ok && rawKey != ""
}

// RequireOrg rejects tokens without an active org. Org-scoped endpoints call
// this before touching any store (ARCHITECTURE.md §4: "no org" is valid for a
// session but not for org resources).
func RequireOrg(claims *SessionClaims) error {
	if claims.OrgID <= 0 {
		return apierrors.Forbidden("no active org: create an org or accept an invite, then switch to it")
	}

	return nil
}
