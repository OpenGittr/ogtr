package handlers

import (
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/opengittr/ogtr/backend/models"
	"github.com/opengittr/ogtr/backend/services"
)

var errAdminDown = errors.New("admin service down")

func newAdminHandler(t *testing.T) (*AdminHandler, *MockAdminService) {
	t.Helper()

	ctrl := gomock.NewController(t)
	admin := NewMockAdminService(ctrl)

	return NewAdminHandler(admin), admin
}

func TestAdminHandler_Users(t *testing.T) {
	page := &services.AdminUsersPage{Users: []models.AdminUser{{ID: 1}}, Total: 1}

	tests := []struct {
		desc     string
		target   string
		expect   func(m *MockAdminService)
		wantErr  bool
		wantCode int
		wantNil  bool
	}{
		{
			desc: "defaults to page 1 with empty query", target: "/api/internal/users",
			expect: func(m *MockAdminService) { m.EXPECT().Users(gomock.Any(), "", 1).Return(page, nil) },
		},
		{
			desc: "query and page pass through", target: "/api/internal/users?query=ac&page=3",
			expect: func(m *MockAdminService) { m.EXPECT().Users(gomock.Any(), "ac", 3).Return(page, nil) },
		},
		{
			desc: "non-numeric page is 400", target: "/api/internal/users?page=x",
			wantErr: true, wantCode: http.StatusBadRequest, wantNil: true,
		},
		{
			desc: "zero page is 400", target: "/api/internal/users?page=0",
			wantErr: true, wantCode: http.StatusBadRequest, wantNil: true,
		},
		{
			// Untyped-nil regression: a failing service must yield an untyped
			// nil result, or gofr answers 206.
			desc: "service error returns untyped nil", target: "/api/internal/users",
			expect: func(m *MockAdminService) {
				m.EXPECT().Users(gomock.Any(), "", 1).Return(nil, errAdminDown)
			},
			wantErr: true, wantNil: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			h, admin := newAdminHandler(t)
			if tc.expect != nil {
				tc.expect(admin)
			}

			ctx := newTestCtx(t, http.MethodGet, tc.target, "", nil, nil)

			result, err := h.Users(ctx)
			if tc.wantErr {
				require.Error(t, err)

				if tc.wantCode != 0 {
					sc, ok := err.(interface{ StatusCode() int })
					require.True(t, ok)
					assert.Equal(t, tc.wantCode, sc.StatusCode())
				}
			} else {
				require.NoError(t, err)
			}

			if tc.wantNil {
				assert.Nil(t, result)
			} else {
				assert.Equal(t, page, result)
			}
		})
	}
}

func TestAdminHandler_Orgs(t *testing.T) {
	h, admin := newAdminHandler(t)

	page := &services.AdminOrgsPage{Orgs: []models.AdminOrg{{ID: 10, Clicks30d: 90}}, Total: 1}
	admin.EXPECT().Orgs(gomock.Any(), "acme", 2).Return(page, nil)

	ctx := newTestCtx(t, http.MethodGet, "/api/internal/orgs?query=acme&page=2", "", nil, nil)

	result, err := h.Orgs(ctx)
	require.NoError(t, err)
	assert.Equal(t, page, result)
}

func TestAdminHandler_Orgs_ServiceErrorIsUntypedNil(t *testing.T) {
	h, admin := newAdminHandler(t)
	admin.EXPECT().Orgs(gomock.Any(), "", 1).Return(nil, errAdminDown)

	ctx := newTestCtx(t, http.MethodGet, "/api/internal/orgs", "", nil, nil)

	result, err := h.Orgs(ctx)
	require.Error(t, err)
	assert.Nil(t, result)
}

func TestAdminHandler_OrgUsers(t *testing.T) {
	page := &services.AdminOrgUsersPage{Users: []models.AdminOrgUser{{ID: 5, Role: models.RoleOwner}}}

	tests := []struct {
		desc     string
		id       string
		expect   func(m *MockAdminService)
		wantErr  bool
		wantCode int
		wantNil  bool
	}{
		{
			desc: "org member list passes through", id: "10",
			expect: func(m *MockAdminService) { m.EXPECT().OrgUsers(gomock.Any(), int64(10)).Return(page, nil) },
		},
		{
			desc: "non-numeric id is 400", id: "x",
			wantErr: true, wantCode: http.StatusBadRequest, wantNil: true,
		},
		{
			desc: "service error returns untyped nil", id: "10",
			expect: func(m *MockAdminService) {
				m.EXPECT().OrgUsers(gomock.Any(), int64(10)).Return(nil, errAdminDown)
			},
			wantErr: true, wantNil: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			h, admin := newAdminHandler(t)
			if tc.expect != nil {
				tc.expect(admin)
			}

			ctx := newTestCtx(t, http.MethodGet, "/api/internal/orgs/"+tc.id+"/users", "", nil,
				map[string]string{"id": tc.id})

			result, err := h.OrgUsers(ctx)
			if tc.wantErr {
				require.Error(t, err)

				if tc.wantCode != 0 {
					sc, ok := err.(interface{ StatusCode() int })
					require.True(t, ok)
					assert.Equal(t, tc.wantCode, sc.StatusCode())
				}
			} else {
				require.NoError(t, err)
			}

			if tc.wantNil {
				assert.Nil(t, result)
			} else {
				assert.Equal(t, page, result)
			}
		})
	}
}

func TestAdminHandler_Reports(t *testing.T) {
	h, admin := newAdminHandler(t)

	page := &services.AdminReportsPage{Reports: []models.AdminReport{{ID: 6}}, Total: 6}
	admin.EXPECT().Reports(gomock.Any(), 1).Return(page, nil)

	ctx := newTestCtx(t, http.MethodGet, "/api/internal/reports", "", nil, nil)

	result, err := h.Reports(ctx)
	require.NoError(t, err)
	assert.Equal(t, page, result)
}

func TestAdminHandler_Link(t *testing.T) {
	tests := []struct {
		desc    string
		id      string
		expect  func(m *MockAdminService)
		wantErr bool
	}{
		{
			desc: "fetches by path id", id: "9",
			expect: func(m *MockAdminService) {
				m.EXPECT().Link(gomock.Any(), int64(9)).Return(&models.AdminLinkDetail{ID: 9}, nil)
			},
		},
		{desc: "malformed id is 400", id: "abc", wantErr: true},
		{desc: "non-positive id is 400", id: "0", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			h, admin := newAdminHandler(t)
			if tc.expect != nil {
				tc.expect(admin)
			}

			ctx := newTestCtx(t, http.MethodGet, "/api/internal/links/"+tc.id, "", nil,
				map[string]string{"id": tc.id})

			result, err := h.Link(ctx)
			if tc.wantErr {
				require.Error(t, err)
				assert.Nil(t, result)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, &models.AdminLinkDetail{ID: 9}, result)
		})
	}
}

func TestAdminHandler_DisableLink(t *testing.T) {
	disabled := &models.AdminLinkDetail{ID: 9, Status: models.LinkStatusDisabledAbuse}

	tests := []struct {
		desc    string
		body    string
		expect  func(m *MockAdminService)
		wantErr bool
	}{
		{
			desc: "reason passes through", body: `{"reason":"operator triage"}`,
			expect: func(m *MockAdminService) {
				m.EXPECT().DisableLink(gomock.Any(), int64(9), "operator triage").Return(disabled, nil)
			},
		},
		{
			desc: "empty body means no reason", body: "",
			expect: func(m *MockAdminService) {
				m.EXPECT().DisableLink(gomock.Any(), int64(9), "").Return(disabled, nil)
			},
		},
		{
			desc: "empty JSON object means no reason", body: `{}`,
			expect: func(m *MockAdminService) {
				m.EXPECT().DisableLink(gomock.Any(), int64(9), "").Return(disabled, nil)
			},
		},
		{desc: "garbage body is 400", body: `{"reason":`, wantErr: true},
		{
			desc: "service error returns untyped nil", body: `{}`,
			expect: func(m *MockAdminService) {
				m.EXPECT().DisableLink(gomock.Any(), int64(9), "").Return(nil, errAdminDown)
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			h, admin := newAdminHandler(t)
			if tc.expect != nil {
				tc.expect(admin)
			}

			ctx := newTestCtx(t, http.MethodPost, "/api/internal/links/9/disable", tc.body, nil,
				map[string]string{"id": "9"})

			result, err := h.DisableLink(ctx)
			if tc.wantErr {
				require.Error(t, err)
				assert.Nil(t, result)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, disabled, result)
		})
	}
}

func TestAdminHandler_EnableLink(t *testing.T) {
	h, admin := newAdminHandler(t)

	enabled := &models.AdminLinkDetail{ID: 9, Status: models.LinkStatusActive}
	admin.EXPECT().EnableLink(gomock.Any(), int64(9)).Return(enabled, nil)

	ctx := newTestCtx(t, http.MethodPost, "/api/internal/links/9/enable", "", nil,
		map[string]string{"id": "9"})

	result, err := h.EnableLink(ctx)
	require.NoError(t, err)
	assert.Equal(t, enabled, result)
}

func TestAdminHandler_DailyStats(t *testing.T) {
	stats := &services.AdminDailyStats{Days: []models.AdminDayStat{{Date: "2026-07-22"}}}

	tests := []struct {
		desc    string
		target  string
		expect  func(m *MockAdminService)
		wantErr bool
	}{
		{
			desc: "missing days delegates the default", target: "/api/internal/stats/daily",
			expect: func(m *MockAdminService) {
				m.EXPECT().DailyStats(gomock.Any(), 0).Return(stats, nil)
			},
		},
		{
			desc: "days passes through", target: "/api/internal/stats/daily?days=90",
			expect: func(m *MockAdminService) {
				m.EXPECT().DailyStats(gomock.Any(), 90).Return(stats, nil)
			},
		},
		{desc: "non-numeric days is 400", target: "/api/internal/stats/daily?days=x", wantErr: true},
		{desc: "non-positive days is 400", target: "/api/internal/stats/daily?days=0", wantErr: true},
		{
			desc: "service error returns untyped nil", target: "/api/internal/stats/daily",
			expect: func(m *MockAdminService) {
				m.EXPECT().DailyStats(gomock.Any(), 0).Return(nil, errAdminDown)
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			h, admin := newAdminHandler(t)
			if tc.expect != nil {
				tc.expect(admin)
			}

			ctx := newTestCtx(t, http.MethodGet, tc.target, "", nil, nil)

			result, err := h.DailyStats(ctx)
			if tc.wantErr {
				require.Error(t, err)
				assert.Nil(t, result)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, stats, result)
		})
	}
}
