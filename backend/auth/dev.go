package auth

import (
	"encoding/json"
	"net/mail"
	"strings"

	"gofr.dev/pkg/gofr"

	"github.com/opengittr/ogtr/backend/apierrors"
)

// DevCredential is the "credential" the dev provider verifies: the plain
// email/name pair from POST /api/v1/auth/dev, JSON-encoded by the handler
// (EncodeDevCredential). There is no cryptographic proof — the provider
// simply trusts the submitted identity, which is exactly why it is only
// functional when AUTH_PROVIDERS includes "dev" and must never be enabled
// in production.
type DevCredential struct {
	Email string `json:"email"`
	Name  string `json:"name"`
}

// EncodeDevCredential packs an email/name pair into the credential string
// carried through the provider-independent login path (IdentityProvider takes
// an opaque credential string; for Google it is the ID token, for dev it is
// this JSON).
func EncodeDevCredential(email, name string) string {
	raw, _ := json.Marshal(DevCredential{Email: email, Name: name}) // cannot fail: two strings

	return string(raw)
}

// DevProvider implements IdentityProvider for local evaluation: it accepts
// any well-formed email + non-empty name with no external identity check, so
// a fresh install works with zero Google OAuth setup.
type DevProvider struct{}

// NewDevProvider builds a DevProvider.
func NewDevProvider() *DevProvider { return &DevProvider{} }

// Verify implements IdentityProvider. Semantic validation lives here (not in
// the handler) so the dev flow exercises the exact same handler→service→
// provider path as Google; invalid input is a 422.
func (*DevProvider) Verify(ctx *gofr.Context, credential string) (Identity, error) {
	var cred DevCredential
	if err := json.Unmarshal([]byte(credential), &cred); err != nil {
		return Identity{}, apierrors.Unprocessable("invalid dev credential")
	}

	email := strings.TrimSpace(cred.Email)
	name := strings.TrimSpace(cred.Name)

	if name == "" {
		return Identity{}, apierrors.Unprocessable("name must not be empty")
	}

	// A bare RFC 5322 address only — no display names, groups or comments.
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Address != email {
		return Identity{}, apierrors.Unprocessable("email is not a valid email address")
	}

	ctx.Logger.Warnf("dev login accepted for %s (no credential proof — dev provider)", email)

	return Identity{Email: email, Name: name}, nil
}
