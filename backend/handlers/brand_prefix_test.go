package handlers

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/opengittr/ogtr/backend/apierrors"
	"github.com/opengittr/ogtr/backend/models"
	"github.com/opengittr/ogtr/backend/services"
)

func newBrandPrefixHandler(t *testing.T) (*BrandPrefixHandler, *MockBrandPrefixService) {
	t.Helper()

	svc := NewMockBrandPrefixService(gomock.NewController(t))

	return NewBrandPrefixHandler(svc), svc
}

func TestBrandPrefixHandler_Create(t *testing.T) {
	created := &models.BrandPrefix{ID: 5, OrgID: 3, Prefix: "ACMEIN", Status: models.BrandPrefixStatusPending}
	body := `{"prefix":"ACMEIN","entity_id":"1101","header":"ACMEIN","entity_name":"Acme"}`

	tests := []struct {
		desc    string
		body    string
		orgless bool
		setup   func(svc *MockBrandPrefixService)
		wantErr bool
	}{
		{
			desc: "request filed",
			body: body,
			setup: func(svc *MockBrandPrefixService) {
				svc.EXPECT().Create(gomock.Any(), int64(3), int64(7), services.BrandPrefixInput{
					Prefix: "ACMEIN", EntityID: "1101", Header: "ACMEIN", EntityName: "Acme",
				}).Return(created, nil)
			},
		},
		{desc: "missing prefix", body: `{}`, wantErr: true},
		{desc: "invalid body", body: `{`, wantErr: true},
		{desc: "org-less token rejected", body: body, orgless: true, wantErr: true},
		{
			desc: "service conflict surfaces",
			body: body,
			setup: func(svc *MockBrandPrefixService) {
				svc.EXPECT().Create(gomock.Any(), int64(3), int64(7), gomock.Any()).
					Return(nil, apierrors.Conflict("this prefix is already taken"))
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			h, svc := newBrandPrefixHandler(t)
			if tc.setup != nil {
				tc.setup(svc)
			}

			claims := orgOwnerClaims()
			if tc.orgless {
				claims = orglessClaims()
			}

			ctx := newTestCtx(t, http.MethodPost, "/api/v1/org/prefixes", tc.body, claims, nil)

			got, err := h.Create(ctx)

			if tc.wantErr {
				require.Error(t, err)
				assert.Nil(t, got, "untyped nil on error")

				return
			}

			require.NoError(t, err)
			assert.Equal(t, created, got)
		})
	}
}

func TestBrandPrefixHandler_ListPrimaryDelete(t *testing.T) {
	h, svc := newBrandPrefixHandler(t)
	approved := &models.BrandPrefix{ID: 5, OrgID: 3, Prefix: "ACMEIN", Status: models.BrandPrefixStatusApproved}

	svc.EXPECT().List(gomock.Any(), int64(3)).Return([]models.BrandPrefix{*approved}, nil)
	svc.EXPECT().SetPrimary(gomock.Any(), int64(3), int64(7), int64(5)).Return(approved, nil)
	svc.EXPECT().Delete(gomock.Any(), int64(3), int64(7), int64(5)).Return(nil)

	list, err := h.List(newTestCtx(t, http.MethodGet, "/api/v1/org/prefixes", "", orgOwnerClaims(), nil))
	require.NoError(t, err)
	assert.Len(t, list, 1)

	vars := map[string]string{"id": "5"}

	primary, err := h.SetPrimary(newTestCtx(t, http.MethodPut, "/api/v1/org/prefixes/5/primary", "", orgOwnerClaims(), vars))
	require.NoError(t, err)
	assert.Equal(t, approved, primary)

	deleted, err := h.Delete(newTestCtx(t, http.MethodDelete, "/api/v1/org/prefixes/5", "", orgOwnerClaims(), vars))
	require.NoError(t, err)
	assert.Equal(t, map[string]bool{"deleted": true}, deleted)

	_, err = h.SetPrimary(newTestCtx(t, http.MethodPut, "/api/v1/org/prefixes/x/primary", "", orgOwnerClaims(),
		map[string]string{"id": "x"}))
	require.Error(t, err, "non-numeric id is 400")
}

func TestAdminHandler_PrefixDecisions(t *testing.T) {
	approved := &models.AdminBrandPrefix{BrandPrefix: models.BrandPrefix{ID: 5, Status: models.BrandPrefixStatusApproved}}
	vars := map[string]string{"id": "5"}

	t.Run("queue passes status and page", func(t *testing.T) {
		h, admin := newAdminHandler(t)
		admin.EXPECT().Prefixes(gomock.Any(), "PENDING", 2).Return(&services.AdminPrefixesPage{}, nil)

		_, err := h.Prefixes(newTestCtx(t, http.MethodGet, "/api/internal/prefixes?status=PENDING&page=2", "", nil, nil))

		require.NoError(t, err)
	})

	t.Run("approve", func(t *testing.T) {
		h, admin := newAdminHandler(t)
		admin.EXPECT().ApprovePrefix(gomock.Any(), int64(5)).Return(approved, nil)

		got, err := h.ApprovePrefix(newTestCtx(t, http.MethodPost, "/api/internal/prefixes/5/approve", "", nil, vars))

		require.NoError(t, err)
		assert.Equal(t, approved, got)
	})

	t.Run("reject passes the reason", func(t *testing.T) {
		h, admin := newAdminHandler(t)
		admin.EXPECT().RejectPrefix(gomock.Any(), int64(5), "not your header").Return(approved, nil)

		_, err := h.RejectPrefix(newTestCtx(t, http.MethodPost, "/api/internal/prefixes/5/reject",
			`{"reason":"not your header"}`, nil, vars))

		require.NoError(t, err)
	})

	t.Run("revoke with garbage body is 400", func(t *testing.T) {
		h, _ := newAdminHandler(t)

		_, err := h.RevokePrefix(newTestCtx(t, http.MethodPost, "/api/internal/prefixes/5/revoke", `{`, nil, vars))

		require.Error(t, err)
	})

	t.Run("release", func(t *testing.T) {
		h, admin := newAdminHandler(t)
		admin.EXPECT().ReleasePrefix(gomock.Any(), int64(5)).Return(nil)

		got, err := h.ReleasePrefix(newTestCtx(t, http.MethodPost, "/api/internal/prefixes/5/release", "", nil, vars))

		require.NoError(t, err)
		assert.Equal(t, map[string]bool{"released": true}, got)
	})
}

func TestResolveHandler_RedirectPassesPrefix(t *testing.T) {
	ctrl := gomock.NewController(t)
	svc := NewMockResolveService(ctrl)
	h := NewResolveHandler(svc, NewMockAPIKeyService(ctrl), "", "sho.rt", "")

	svc.EXPECT().Resolve(gomock.Any(), "ACMEIN", "abc1234", "", gomock.Any()).
		Return(&services.Resolution{Code: "abc1234", URL: "https://example.com"}, nil)

	ctx := newTestCtx(t, http.MethodGet, "/ACMEIN/abc1234", "", nil,
		map[string]string{"prefix": "ACMEIN", "code": "abc1234"})

	_, err := h.Redirect(ctx)

	require.NoError(t, err)
}
