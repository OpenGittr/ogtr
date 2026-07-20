package handlers

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/opengittr/ogtr/backend/models"
	"github.com/opengittr/ogtr/backend/services"
)

func newAPIKeyHandler(t *testing.T) (*APIKeyHandler, *MockAPIKeyService) {
	t.Helper()

	svc := NewMockAPIKeyService(gomock.NewController(t))

	return NewAPIKeyHandler(svc), svc
}

func TestAPIKeyHandler_Create(t *testing.T) {
	created := &services.CreatedAPIKey{
		APIKey: models.APIKey{ID: 11, OrgID: 3, Name: "ci key", KeyHint: "slk_Ab12Cd34", Status: "ENABLED"},
		Key:    "slk_Ab12Cd34EfGh56Ij78Kl90Mn12Op34Qr56St78Uv90",
	}

	tests := []struct {
		desc    string
		body    string
		orgless bool
		setup   func(svc *MockAPIKeyService)
		wantErr bool
	}{
		{
			desc: "created with plaintext key",
			body: `{"name":"ci key"}`,
			setup: func(svc *MockAPIKeyService) {
				svc.EXPECT().Create(gomock.Any(), int64(3), "ci key").Return(created, nil)
			},
		},
		{desc: "missing name", body: `{}`, wantErr: true},
		{desc: "invalid body", body: `{`, wantErr: true},
		{desc: "org-less token rejected", body: `{"name":"k"}`, orgless: true, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			h, svc := newAPIKeyHandler(t)
			if tc.setup != nil {
				tc.setup(svc)
			}

			claims := orgOwnerClaims()
			if tc.orgless {
				claims = orglessClaims()
			}

			ctx := newTestCtx(t, http.MethodPost, "/api/v1/api-keys", tc.body, claims, nil)

			got, err := h.Create(ctx)

			if tc.wantErr {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, created, got)
		})
	}
}

// TestAPIKeyHandler_Create_UntypedNilOnError pins the gofr 206 regression: on
// a service error the handler must return exactly nil, not the service's
// typed nil pointer.
func TestAPIKeyHandler_Create_UntypedNilOnError(t *testing.T) {
	h, svc := newAPIKeyHandler(t)

	svc.EXPECT().Create(gomock.Any(), int64(3), "k").Return(nil, assert.AnError)

	ctx := newTestCtx(t, http.MethodPost, "/api/v1/api-keys", `{"name":"k"}`, orgOwnerClaims(), nil)

	got, err := h.Create(ctx)
	require.Error(t, err)
	assert.Nil(t, got)
}

func TestAPIKeyHandler_List(t *testing.T) {
	h, svc := newAPIKeyHandler(t)

	keys := []models.APIKey{{ID: 11, OrgID: 3, Name: "ci key", KeyHint: "slk_Ab12Cd34", Status: "ENABLED"}}
	svc.EXPECT().List(gomock.Any(), int64(3)).Return(keys, nil)

	ctx := newTestCtx(t, http.MethodGet, "/api/v1/api-keys", "", orgOwnerClaims(), nil)

	got, err := h.List(ctx)
	require.NoError(t, err)
	assert.Len(t, got.([]models.APIKey), 1)
}

// TestAPIKeyHandler_List_UntypedNilOnError pins the gofr 206 regression for
// the slice-returning endpoint.
func TestAPIKeyHandler_List_UntypedNilOnError(t *testing.T) {
	h, svc := newAPIKeyHandler(t)

	svc.EXPECT().List(gomock.Any(), int64(3)).Return(nil, assert.AnError)

	ctx := newTestCtx(t, http.MethodGet, "/api/v1/api-keys", "", orgOwnerClaims(), nil)

	got, err := h.List(ctx)
	require.Error(t, err)
	assert.Nil(t, got)
}

func TestAPIKeyHandler_List_Orgless(t *testing.T) {
	h, _ := newAPIKeyHandler(t)

	ctx := newTestCtx(t, http.MethodGet, "/api/v1/api-keys", "", orglessClaims(), nil)

	_, err := h.List(ctx)
	require.Error(t, err)
}

func TestAPIKeyHandler_Disable(t *testing.T) {
	disabled := &models.APIKey{ID: 11, OrgID: 3, Name: "ci key", KeyHint: "slk_Ab12Cd34", Status: "DISABLED"}

	tests := []struct {
		desc    string
		id      string
		setup   func(svc *MockAPIKeyService)
		wantErr bool
	}{
		{
			desc: "disabled",
			id:   "11",
			setup: func(svc *MockAPIKeyService) {
				svc.EXPECT().Disable(gomock.Any(), int64(3), int64(11)).Return(disabled, nil)
			},
		},
		{desc: "bad id", id: "x", wantErr: true},
		{
			desc: "service error returns untyped nil",
			id:   "11",
			setup: func(svc *MockAPIKeyService) {
				svc.EXPECT().Disable(gomock.Any(), int64(3), int64(11)).Return(nil, assert.AnError)
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			h, svc := newAPIKeyHandler(t)
			if tc.setup != nil {
				tc.setup(svc)
			}

			ctx := newTestCtx(t, http.MethodDelete, "/api/v1/api-keys/"+tc.id, "", orgOwnerClaims(),
				map[string]string{"id": tc.id})

			got, err := h.Disable(ctx)

			if tc.wantErr {
				require.Error(t, err)
				assert.Nil(t, got)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, disabled, got)
		})
	}
}
