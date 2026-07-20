// Package auth implements the identity-provider boundary (ARCHITECTURE.md §5),
// ogtr's own session tokens (§4), and the HTTP auth middleware.
package auth

import "gofr.dev/pkg/gofr"

//go:generate mockgen -source=provider.go -destination=mock_provider.go -package=auth

// Identity is the proven identity returned by an IdentityProvider.
type Identity struct {
	Email string
	Name  string
}

// IdentityProvider verifies a provider-issued credential and returns the
// proven identity. GoogleProvider ships today; future enterprise IdP
// providers slot in behind the same interface. Everything after Verify
// (JIT user creation, org resolution, ogtr JWT issuance) is
// provider-independent.
type IdentityProvider interface {
	Verify(ctx *gofr.Context, credential string) (Identity, error)
}
