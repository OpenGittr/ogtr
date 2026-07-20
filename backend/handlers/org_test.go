package handlers

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/opengittr/ogtr/backend/apierrors"
	"github.com/opengittr/ogtr/backend/auth"
	"github.com/opengittr/ogtr/backend/models"
	"github.com/opengittr/ogtr/backend/services"
)

func newOrgHandler(t *testing.T) (*OrgHandler, *MockOrgService, *MockAuthService) {
	t.Helper()

	ctrl := gomock.NewController(t)
	orgSvc := NewMockOrgService(ctrl)
	authSvc := NewMockAuthService(ctrl)

	return NewOrgHandler(orgSvc, authSvc), orgSvc, authSvc
}

func TestOrgHandler_Create(t *testing.T) {
	org := &models.Org{ID: 3, Name: "Acme", Slug: "acme"}
	pair := models.TokenPair{AccessToken: "a", RefreshToken: "r"}

	tests := []struct {
		desc    string
		body    string
		claims  *auth.SessionClaims
		setup   func(orgSvc *MockOrgService, authSvc *MockAuthService)
		wantErr bool
	}{
		{
			desc:   "org-less user creates their first org",
			body:   `{"name":"Acme"}`,
			claims: orglessClaims(),
			setup: func(orgSvc *MockOrgService, authSvc *MockAuthService) {
				orgSvc.EXPECT().Create(gomock.Any(), int64(7), "Acme", "").Return(org, nil)
				authSvc.EXPECT().SwitchOrg(gomock.Any(), int64(7), int64(3)).Return(pair, nil)
			},
		},
		{
			desc:    "missing name",
			body:    `{}`,
			claims:  orglessClaims(),
			setup:   func(*MockOrgService, *MockAuthService) {},
			wantErr: true,
		},
		{
			desc:    "unauthenticated",
			body:    `{"name":"Acme"}`,
			claims:  nil,
			setup:   func(*MockOrgService, *MockAuthService) {},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			h, orgSvc, authSvc := newOrgHandler(t)
			tc.setup(orgSvc, authSvc)

			ctx := newTestCtx(t, http.MethodPost, "/api/v1/orgs", tc.body, tc.claims, nil)

			got, err := h.Create(ctx)

			if tc.wantErr {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)

			resp, ok := got.(createOrgResponse)
			require.True(t, ok)
			assert.Equal(t, org, resp.Org)
			assert.Equal(t, pair, resp.TokenPair)
		})
	}
}

// A limits-policy denial from the service must reach Gofr untouched (status
// 403 + code LIMIT_REACHED + message) with an UNTYPED nil body — a typed nil
// alongside an error would make Gofr respond 206 (ARCHITECTURE.md §8).
func TestOrgHandler_Create_LimitReached(t *testing.T) {
	h, orgSvc, _ := newOrgHandler(t)
	denial := apierrors.LimitReached("org limit reached for this account")

	orgSvc.EXPECT().Create(gomock.Any(), int64(7), "Acme", "").Return(nil, denial)

	ctx := newTestCtx(t, http.MethodPost, "/api/v1/orgs", `{"name":"Acme"}`, orglessClaims(), nil)

	got, err := h.Create(ctx)

	assert.True(t, got == nil, "handler must return an untyped nil on error")
	require.Error(t, err)
	assert.Equal(t, denial, err)

	var apiErr apierrors.Error
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, http.StatusForbidden, apiErr.StatusCode())
	assert.Equal(t, map[string]any{"code": apierrors.CodeLimitReached}, apiErr.Response())
	assert.Equal(t, "org limit reached for this account", apiErr.Error())
}

func TestOrgHandler_Get(t *testing.T) {
	tests := []struct {
		desc    string
		claims  *auth.SessionClaims
		setup   func(orgSvc *MockOrgService)
		wantErr bool
	}{
		{
			desc:   "org token fetches its org",
			claims: orgOwnerClaims(),
			setup: func(orgSvc *MockOrgService) {
				orgSvc.EXPECT().Get(gomock.Any(), int64(3)).Return(&models.Org{ID: 3}, nil)
			},
		},
		{
			desc:    "org-less token is rejected",
			claims:  orglessClaims(),
			setup:   func(*MockOrgService) {},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			h, orgSvc, _ := newOrgHandler(t)
			tc.setup(orgSvc)

			ctx := newTestCtx(t, http.MethodGet, "/api/v1/org", "", tc.claims, nil)

			_, err := h.Get(ctx)
			assert.Equal(t, tc.wantErr, err != nil)
		})
	}
}

func TestOrgHandler_Update(t *testing.T) {
	h, orgSvc, _ := newOrgHandler(t)

	name := "Renamed"
	orgSvc.EXPECT().Update(gomock.Any(), int64(3), int64(7), services.OrgUpdate{Name: &name}).
		Return(&models.Org{ID: 3, Name: "Renamed"}, nil)

	ctx := newTestCtx(t, http.MethodPatch, "/api/v1/org", `{"name":"Renamed"}`, orgOwnerClaims(), nil)

	got, err := h.Update(ctx)
	require.NoError(t, err)
	assert.Equal(t, "Renamed", got.(*models.Org).Name)
}

func TestOrgHandler_Members(t *testing.T) {
	h, orgSvc, _ := newOrgHandler(t)

	orgSvc.EXPECT().Members(gomock.Any(), int64(3)).Return([]models.Member{{UserID: 7}}, nil)

	ctx := newTestCtx(t, http.MethodGet, "/api/v1/org/members", "", orgOwnerClaims(), nil)

	got, err := h.Members(ctx)
	require.NoError(t, err)
	assert.Len(t, got.([]models.Member), 1)
}

// TestOrgHandler_Members_UntypedNilOnError pins the gofr 206 regression: on a
// service error the handler must return exactly nil, not the service's typed
// nil slice (a typed nil in the any return makes gofr respond 206).
func TestOrgHandler_Members_UntypedNilOnError(t *testing.T) {
	h, orgSvc, _ := newOrgHandler(t)

	orgSvc.EXPECT().Members(gomock.Any(), int64(3)).Return(nil, assert.AnError)

	ctx := newTestCtx(t, http.MethodGet, "/api/v1/org/members", "", orgOwnerClaims(), nil)

	got, err := h.Members(ctx)
	require.Error(t, err)
	assert.Nil(t, got)
}

func TestOrgHandler_RemoveMember(t *testing.T) {
	tests := []struct {
		desc    string
		vars    map[string]string
		setup   func(orgSvc *MockOrgService)
		wantErr bool
	}{
		{
			desc: "valid removal",
			vars: map[string]string{"userId": "9"},
			setup: func(orgSvc *MockOrgService) {
				orgSvc.EXPECT().RemoveMember(gomock.Any(), int64(3), int64(7), int64(9)).Return(nil)
			},
		},
		{
			desc:    "non-numeric user id",
			vars:    map[string]string{"userId": "abc"},
			setup:   func(*MockOrgService) {},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			h, orgSvc, _ := newOrgHandler(t)
			tc.setup(orgSvc)

			ctx := newTestCtx(t, http.MethodDelete, "/api/v1/org/members/x", "", orgOwnerClaims(), tc.vars)

			_, err := h.RemoveMember(ctx)
			assert.Equal(t, tc.wantErr, err != nil)
		})
	}
}

func TestOrgHandler_Invites(t *testing.T) {
	invite := &models.Invite{ID: 12, OrgID: 3, Email: "x@y.co"}

	t.Run("create", func(t *testing.T) {
		h, orgSvc, _ := newOrgHandler(t)
		orgSvc.EXPECT().CreateInvite(gomock.Any(), int64(3), int64(7), "x@y.co").Return(invite, nil)

		ctx := newTestCtx(t, http.MethodPost, "/api/v1/org/invites", `{"email":"x@y.co"}`, orgOwnerClaims(), nil)

		got, err := h.CreateInvite(ctx)
		require.NoError(t, err)
		assert.Equal(t, invite, got)
	})

	t.Run("create with missing email", func(t *testing.T) {
		h, _, _ := newOrgHandler(t)

		ctx := newTestCtx(t, http.MethodPost, "/api/v1/org/invites", `{}`, orgOwnerClaims(), nil)

		_, err := h.CreateInvite(ctx)
		require.Error(t, err)
	})

	t.Run("list", func(t *testing.T) {
		h, orgSvc, _ := newOrgHandler(t)
		orgSvc.EXPECT().ListInvites(gomock.Any(), int64(3)).Return([]models.Invite{*invite}, nil)

		ctx := newTestCtx(t, http.MethodGet, "/api/v1/org/invites", "", orgOwnerClaims(), nil)

		got, err := h.ListInvites(ctx)
		require.NoError(t, err)
		assert.Len(t, got.([]models.Invite), 1)
	})

	t.Run("list returns untyped nil on service error (gofr 206 regression)", func(t *testing.T) {
		h, orgSvc, _ := newOrgHandler(t)
		orgSvc.EXPECT().ListInvites(gomock.Any(), int64(3)).Return(nil, assert.AnError)

		ctx := newTestCtx(t, http.MethodGet, "/api/v1/org/invites", "", orgOwnerClaims(), nil)

		got, err := h.ListInvites(ctx)
		require.Error(t, err)
		assert.Nil(t, got)
	})

	t.Run("revoke", func(t *testing.T) {
		h, orgSvc, _ := newOrgHandler(t)
		orgSvc.EXPECT().RevokeInvite(gomock.Any(), int64(3), int64(7), int64(12)).Return(nil)

		ctx := newTestCtx(t, http.MethodDelete, "/api/v1/org/invites/12", "", orgOwnerClaims(),
			map[string]string{"id": "12"})

		_, err := h.RevokeInvite(ctx)
		require.NoError(t, err)
	})

	t.Run("org-less token rejected on invites", func(t *testing.T) {
		h, _, _ := newOrgHandler(t)

		ctx := newTestCtx(t, http.MethodGet, "/api/v1/org/invites", "", orglessClaims(), nil)

		_, err := h.ListInvites(ctx)
		require.Error(t, err)
	})
}
