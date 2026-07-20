package services

import (
	"context"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"gofr.dev/pkg/gofr"
	"gofr.dev/pkg/gofr/container"

	"github.com/opengittr/ogtr/backend/limits"
	"github.com/opengittr/ogtr/backend/models"
)

func newAPIKeyService(t *testing.T) (*APIKeyService, *MockAPIKeyStore, *gofr.Context) {
	t.Helper()

	return newAPIKeyServiceWithPolicy(t, limits.Unlimited{})
}

func newAPIKeyServiceWithPolicy(t *testing.T, policy limits.Policy) (*APIKeyService, *MockAPIKeyStore, *gofr.Context) {
	t.Helper()

	keys := NewMockAPIKeyStore(gomock.NewController(t))

	mockContainer, _ := container.NewMockContainer(t)
	ctx := &gofr.Context{Context: context.Background(), Container: mockContainer}

	return NewAPIKeyService(keys, policy), keys, ctx
}

func enabledKey(id int64) *models.APIKey {
	return &models.APIKey{ID: id, OrgID: 3, Name: "ci key", KeyHint: "slk_Ab12Cd34", Status: models.APIKeyStatusEnabled}
}

var apiKeyRe = regexp.MustCompile(`^slk_[a-zA-Z0-9]{40}$`)

func TestAPIKeyService_Create(t *testing.T) {
	svc, keys, ctx := newAPIKeyService(t)

	var storedHash, storedHint string

	keys.EXPECT().Create(gomock.Any(), int64(3), "ci key", gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ *gofr.Context, _ int64, _, keyHash, keyHint string) (*models.APIKey, error) {
			storedHash, storedHint = keyHash, keyHint

			key := enabledKey(11)
			key.KeyHint = keyHint

			return key, nil
		})

	created, err := svc.Create(ctx, 3, "  ci key  ")

	require.NoError(t, err)
	assert.True(t, apiKeyRe.MatchString(created.Key), "key %q should be slk_ + 40 base62 chars", created.Key)
	assert.Equal(t, hashAPIKey(created.Key), storedHash, "stored hash must be the SHA-256 hex of the returned key")
	assert.Regexp(t, `^[0-9a-f]{64}$`, storedHash, "hash should be SHA-256 hex, never the plaintext")
	assert.Equal(t, created.Key[:12], storedHint, "hint is slk_ + first 8 random chars")
	assert.Equal(t, int64(11), created.ID)
	assert.Equal(t, models.APIKeyStatusEnabled, created.Status)
}

func TestAPIKeyService_Create_KeysDoNotRepeat(t *testing.T) {
	svc, keys, ctx := newAPIKeyService(t)

	seenKeys := map[string]bool{}
	seenHashes := map[string]bool{}

	keys.EXPECT().Create(gomock.Any(), int64(3), "k", gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ *gofr.Context, _ int64, _, keyHash, _ string) (*models.APIKey, error) {
			seenHashes[keyHash] = true

			return enabledKey(11), nil
		}).Times(5)

	for range 5 {
		created, err := svc.Create(ctx, 3, "k")
		require.NoError(t, err)

		seenKeys[created.Key] = true
	}

	assert.Len(t, seenKeys, 5, "keys must not repeat")
	assert.Len(t, seenHashes, 5, "hashes must not repeat")
}

func TestAPIKeyService_Create_Rejections(t *testing.T) {
	tests := []struct {
		desc string
		name string
	}{
		{desc: "empty name", name: "   "},
		{desc: "name too long", name: strings.Repeat("x", 256)},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			svc, _, ctx := newAPIKeyService(t)

			_, err := svc.Create(ctx, 3, tc.name)

			require.Error(t, err)
			assertStatus(t, err, http.StatusUnprocessableEntity)
		})
	}
}

func TestAPIKeyService_List(t *testing.T) {
	svc, keys, ctx := newAPIKeyService(t)

	keys.EXPECT().List(gomock.Any(), int64(3)).Return([]models.APIKey{*enabledKey(11)}, nil)

	got, err := svc.List(ctx, 3)

	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "slk_Ab12Cd34", got[0].KeyHint)
}

func TestAPIKeyService_Disable(t *testing.T) {
	disabled := enabledKey(11)
	disabled.Status = models.APIKeyStatusDisabled

	tests := []struct {
		desc       string
		setup      func(keys *MockAPIKeyStore)
		wantStatus int
	}{
		{
			desc: "enabled key is disabled",
			setup: func(keys *MockAPIKeyStore) {
				keys.EXPECT().GetByID(gomock.Any(), int64(3), int64(11)).Return(enabledKey(11), nil)
				keys.EXPECT().Disable(gomock.Any(), int64(3), int64(11)).Return(nil)
			},
		},
		{
			desc: "already-disabled key is a no-op",
			setup: func(keys *MockAPIKeyStore) {
				k := enabledKey(11)
				k.Status = models.APIKeyStatusDisabled
				keys.EXPECT().GetByID(gomock.Any(), int64(3), int64(11)).Return(k, nil)
			},
		},
		{
			desc: "cross-org key is 404, existence hidden",
			setup: func(keys *MockAPIKeyStore) {
				keys.EXPECT().GetByID(gomock.Any(), int64(3), int64(11)).Return(nil, nil)
			},
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			svc, keys, ctx := newAPIKeyService(t)
			tc.setup(keys)

			got, err := svc.Disable(ctx, 3, 11)

			if tc.wantStatus != 0 {
				require.Error(t, err)
				assertStatus(t, err, tc.wantStatus)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, models.APIKeyStatusDisabled, got.Status)
		})
	}
}

func TestAPIKeyService_Authenticate(t *testing.T) {
	const plaintext = "slk_0123456789012345678901234567890123456789"

	disabled := enabledKey(11)
	disabled.Status = models.APIKeyStatusDisabled

	tests := []struct {
		desc      string
		rawKey    string
		setup     func(keys *MockAPIKeyStore)
		wantErr   bool
		wantTouch bool
	}{
		{
			desc:   "valid enabled key resolves to its org and stamps last_used_at",
			rawKey: plaintext,
			setup: func(keys *MockAPIKeyStore) {
				keys.EXPECT().GetByHash(gomock.Any(), hashAPIKey(plaintext)).Return(enabledKey(11), nil)
				keys.EXPECT().TouchLastUsed(gomock.Any(), int64(11)).Return(nil)
			},
			wantTouch: true,
		},
		{
			desc:   "unknown key is 401",
			rawKey: plaintext,
			setup: func(keys *MockAPIKeyStore) {
				keys.EXPECT().GetByHash(gomock.Any(), hashAPIKey(plaintext)).Return(nil, nil)
			},
			wantErr: true,
		},
		{
			desc:   "disabled key is 401",
			rawKey: plaintext,
			setup: func(keys *MockAPIKeyStore) {
				keys.EXPECT().GetByHash(gomock.Any(), hashAPIKey(plaintext)).Return(disabled, nil)
			},
			wantErr: true,
		},
		{desc: "blank key is 401 without a lookup", rawKey: "   ", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			svc, keys, ctx := newAPIKeyService(t)
			if tc.setup != nil {
				tc.setup(keys)
			}

			got, err := svc.Authenticate(ctx, tc.rawKey)

			// The last_used_at stamp is fire-and-forget; wait so the mock
			// expectation is deterministic under -race.
			svc.touches.Wait()

			if tc.wantErr {
				require.Error(t, err)
				assertStatus(t, err, http.StatusUnauthorized)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, int64(3), got.OrgID)
		})
	}
}

func TestAPIKeyService_Authenticate_TouchFailureDoesNotFailAuth(t *testing.T) {
	const plaintext = "slk_0123456789012345678901234567890123456789"

	svc, keys, ctx := newAPIKeyService(t)
	keys.EXPECT().GetByHash(gomock.Any(), hashAPIKey(plaintext)).Return(enabledKey(11), nil)
	keys.EXPECT().TouchLastUsed(gomock.Any(), int64(11)).Return(assert.AnError)

	got, err := svc.Authenticate(ctx, plaintext)
	svc.touches.Wait()

	require.NoError(t, err, "a failed bookkeeping write must not fail authentication")
	assert.Equal(t, int64(11), got.ID)
}
