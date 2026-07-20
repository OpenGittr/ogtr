package services

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"sync"

	"gofr.dev/pkg/gofr"

	"github.com/opengittr/ogtr/backend/apierrors"
	"github.com/opengittr/ogtr/backend/limits"
	"github.com/opengittr/ogtr/backend/models"
)

const (
	// apiKeyPrefix lets users recognize a ogtr key at a glance.
	apiKeyPrefix = "slk_"
	// apiKeyRandomLength is the number of random base62 characters after the
	// prefix (~238 bits of entropy).
	apiKeyRandomLength = 40
	// apiKeyHintLength is how much of the key the stored display hint keeps:
	// the prefix plus the first 8 random characters.
	apiKeyHintLength = len(apiKeyPrefix) + 8

	maxAPIKeyNameLength = 255
)

// CreatedAPIKey is the create response: the stored row plus the plaintext
// key, returned exactly once — it is never persisted and can never be shown
// again.
type CreatedAPIKey struct {
	models.APIKey
	Key string `json:"key"`
}

// APIKeyService implements developer API keys (FEATURES.md §8): named
// per-org keys, hash-stored, ENABLED/DISABLED lifecycle, and the X-API-Key
// authentication used by link creation and resolution. Any org member can
// manage the org's keys — the spec is silent on roles, so we keep the
// simplest rule (documented in ARCHITECTURE.md §4).
type APIKeyService struct {
	keys   APIKeyStore
	policy limits.Policy

	// touches tracks the fire-and-forget last_used_at updates so tests can
	// wait for them; production never Waits.
	touches sync.WaitGroup
}

// NewAPIKeyService wires an APIKeyService. policy bounds key creation; wire
// limits.Unlimited{} unless the deployment supplies its own.
func NewAPIKeyService(keys APIKeyStore, policy limits.Policy) *APIKeyService {
	return &APIKeyService{keys: keys, policy: policy}
}

// Create generates a key ("slk_" + 40 crypto/rand base62 chars), stores its
// SHA-256 hex digest and display hint, and returns the plaintext exactly once.
func (s *APIKeyService) Create(ctx *gofr.Context, orgID int64, name string) (*CreatedAPIKey, error) {
	name = strings.TrimSpace(name)

	if name == "" {
		return nil, apierrors.Unprocessable("name must not be empty")
	}

	if len(name) > maxAPIKeyNameLength {
		return nil, apierrors.Unprocessable("name must be at most 255 characters")
	}

	// The deployment's limits.Policy gates key creation after input
	// validation and before any store access; a denial is 403 LIMIT_REACHED.
	if err := s.policy.CanCreateAPIKey(ctx, orgID); err != nil {
		return nil, limitError(err)
	}

	random, err := randomCode(apiKeyRandomLength)
	if err != nil {
		return nil, err
	}

	plaintext := apiKeyPrefix + random

	key, err := s.keys.Create(ctx, orgID, name, hashAPIKey(plaintext), plaintext[:apiKeyHintLength])
	if err != nil {
		return nil, err
	}

	ctx.Logger.Infof("api key %d (%q, hint %s) created in org %d", key.ID, key.Name, key.KeyHint, orgID)

	return &CreatedAPIKey{APIKey: *key, Key: plaintext}, nil
}

// List returns the org's keys, newest first — no key material beyond the hint.
func (s *APIKeyService) List(ctx *gofr.Context, orgID int64) ([]models.APIKey, error) {
	return s.keys.List(ctx, orgID)
}

// Disable sets a key to DISABLED. Keys are never hard-deleted: links keep
// their api_key_id attribution. A key from another org is 404 — existence is
// not revealed. Disabling an already-disabled key is a no-op.
func (s *APIKeyService) Disable(ctx *gofr.Context, orgID, id int64) (*models.APIKey, error) {
	key, err := s.keys.GetByID(ctx, orgID, id)
	if err != nil {
		return nil, err
	}

	if key == nil {
		return nil, apierrors.NotFound("api key not found")
	}

	if key.Status != models.APIKeyStatusDisabled {
		if err := s.keys.Disable(ctx, orgID, id); err != nil {
			return nil, err
		}

		ctx.Logger.Infof("api key %d (%q) disabled in org %d", key.ID, key.Name, orgID)
	}

	key.Status = models.APIKeyStatusDisabled

	return key, nil
}

// Authenticate resolves an X-API-Key header value to its key (and thus its
// org). Unknown or disabled keys are 401 — an explicitly supplied wrong key
// always fails loudly, never silently proceeds. On success, last_used_at is
// stamped fire-and-forget: the request never waits on (or fails because of)
// the bookkeeping write.
func (s *APIKeyService) Authenticate(ctx *gofr.Context, rawKey string) (*models.APIKey, error) {
	rawKey = strings.TrimSpace(rawKey)
	if rawKey == "" {
		return nil, apierrors.Unauthorized("invalid API key")
	}

	key, err := s.keys.GetByHash(ctx, hashAPIKey(rawKey))
	if err != nil {
		return nil, err
	}

	if key == nil || key.Status != models.APIKeyStatusEnabled {
		return nil, apierrors.Unauthorized("invalid API key")
	}

	s.touches.Add(1)

	go func() {
		defer s.touches.Done()

		if err := s.keys.TouchLastUsed(ctx, key.ID); err != nil {
			ctx.Logger.Errorf("api key %d last_used_at update failed: %v", key.ID, err)
		}
	}()

	return key, nil
}

// hashAPIKey is the storage form of a key: SHA-256, hex-encoded.
func hashAPIKey(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))

	return hex.EncodeToString(sum[:])
}
