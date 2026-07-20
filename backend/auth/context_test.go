package auth

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/opengittr/ogtr/backend/apierrors"
)

func TestClaimsRoundTrip(t *testing.T) {
	claims := &SessionClaims{UserID: 5, OrgID: 9, Role: "MEMBER"}

	ctx := ContextWithClaims(context.Background(), claims)

	got, err := ClaimsFromContext(ctx)
	require.NoError(t, err)
	assert.Equal(t, claims, got)
}

func TestClaimsFromContext_Missing(t *testing.T) {
	got, err := ClaimsFromContext(context.Background())
	assert.Nil(t, got)
	require.Error(t, err)
	assert.Equal(t, apierrors.Unauthorized("authentication required"), err)
}

func TestRequireOrg(t *testing.T) {
	tests := []struct {
		desc    string
		orgID   int64
		wantErr bool
	}{
		{desc: "org present", orgID: 4, wantErr: false},
		{desc: "org-less token rejected", orgID: 0, wantErr: true},
		{desc: "negative org rejected", orgID: -1, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			err := RequireOrg(&SessionClaims{OrgID: tc.orgID})

			if tc.wantErr {
				require.Error(t, err)

				var apiErr apierrors.Error

				require.ErrorAs(t, err, &apiErr)
				assert.Equal(t, 403, apiErr.StatusCode())
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestAPIKeyContext(t *testing.T) {
	t.Run("round-trips the raw key", func(t *testing.T) {
		ctx := ContextWithAPIKey(context.Background(), "slk_abc")

		key, ok := APIKeyFromContext(ctx)
		require.True(t, ok)
		assert.Equal(t, "slk_abc", key)
	})

	t.Run("absent key reports false", func(t *testing.T) {
		key, ok := APIKeyFromContext(context.Background())
		assert.False(t, ok)
		assert.Empty(t, key)
	})

	t.Run("empty key reports false", func(t *testing.T) {
		_, ok := APIKeyFromContext(ContextWithAPIKey(context.Background(), ""))
		assert.False(t, ok)
	})
}
