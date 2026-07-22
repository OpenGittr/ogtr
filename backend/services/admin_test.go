package services

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"gofr.dev/pkg/gofr"
	"gofr.dev/pkg/gofr/container"

	"github.com/opengittr/ogtr/backend/models"
)

var errBoom = errors.New("store down")

func newAdminService(t *testing.T) (*AdminService, *MockAdminStore, *MockLinkStatusStore, *gofr.Context) {
	t.Helper()

	ctrl := gomock.NewController(t)
	store := NewMockAdminStore(ctrl)
	links := NewMockLinkStatusStore(ctrl)

	mockContainer, _ := container.NewMockContainer(t)
	ctx := &gofr.Context{Context: context.Background(), Container: mockContainer}

	return NewAdminService(store, links), store, links, ctx
}

// fixedAdminNow pins the admin clock for window math.
func fixedAdminNow(t *testing.T, iso string) {
	t.Helper()

	fixed, err := time.Parse(time.RFC3339, iso)
	require.NoError(t, err)

	adminNowFn = func() time.Time { return fixed }

	t.Cleanup(func() { adminNowFn = time.Now })
}

func TestAdminService_Users_AttachesMultiOrgMemberships(t *testing.T) {
	svc, store, _, ctx := newAdminService(t)

	store.EXPECT().ListUsers(ctx, "ac", adminPageSize, 0).Return([]models.AdminUser{
		{ID: 1, Email: "a@x.co", Orgs: []models.AdminUserOrg{}},
		{ID: 2, Email: "b@x.co", Orgs: []models.AdminUserOrg{}},
	}, nil)
	store.EXPECT().CountUsers(ctx, "ac").Return(int64(2), nil)
	store.EXPECT().UserOrgs(ctx, []int64{1, 2}).Return(map[int64][]models.AdminUserOrg{
		1: {{ID: 10, Name: "Acme", Role: "OWNER"}, {ID: 11, Name: "Beta", Role: "MEMBER"}},
	}, nil)

	page, err := svc.Users(ctx, "ac", 1)
	require.NoError(t, err)
	assert.Equal(t, int64(2), page.Total)
	require.Len(t, page.Users, 2)
	// The cross-org read returns multi-org data for one user…
	require.Len(t, page.Users[0].Orgs, 2)
	assert.Equal(t, "Beta", page.Users[0].Orgs[1].Name)
	// …and an org-less user keeps an empty (non-nil) slice.
	assert.NotNil(t, page.Users[1].Orgs)
	assert.Empty(t, page.Users[1].Orgs)
}

func TestAdminService_Users_PaginationMath(t *testing.T) {
	tests := []struct {
		desc       string
		page       int
		wantOffset int
	}{
		{desc: "page 1 is offset 0", page: 1, wantOffset: 0},
		{desc: "page 3 is offset 50", page: 3, wantOffset: 50},
		{desc: "page 0 clamps to the first page", page: 0, wantOffset: 0},
		{desc: "negative page clamps to the first page", page: -4, wantOffset: 0},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			svc, store, _, ctx := newAdminService(t)

			store.EXPECT().ListUsers(ctx, "", adminPageSize, tc.wantOffset).Return([]models.AdminUser{}, nil)
			store.EXPECT().CountUsers(ctx, "").Return(int64(60), nil)
			store.EXPECT().UserOrgs(ctx, []int64{}).Return(map[int64][]models.AdminUserOrg{}, nil)

			page, err := svc.Users(ctx, "", tc.page)
			require.NoError(t, err)
			assert.Equal(t, int64(60), page.Total)
		})
	}
}

func TestAdminService_Users_StoreError(t *testing.T) {
	svc, store, _, ctx := newAdminService(t)
	store.EXPECT().ListUsers(ctx, "", adminPageSize, 0).Return(nil, errBoom)

	page, err := svc.Users(ctx, "", 1)
	require.Error(t, err)
	assert.Nil(t, page)
}

func TestAdminService_Orgs_FillsGroupedCounts(t *testing.T) {
	svc, store, _, ctx := newAdminService(t)

	fixedAdminNow(t, "2026-07-22T10:00:00Z")

	store.EXPECT().ListOrgs(ctx, "", adminPageSize, adminPageSize).Return([]models.AdminOrg{
		{ID: 10, Name: "Acme", Slug: "acme"},
		{ID: 11, Name: "Beta", Slug: "beta"},
	}, nil)
	store.EXPECT().CountOrgs(ctx, "").Return(int64(27), nil)
	// The clicks window is exactly now−30d, UTC.
	store.EXPECT().OrgCounts(ctx, []int64{10, 11}, "2026-06-22 10:00:00").
		Return(map[int64]models.AdminOrgCounts{
			10: {Members: 3, Links: 7, Clicks30d: 90, Domains: 1},
		}, nil)

	page, err := svc.Orgs(ctx, "", 2)
	require.NoError(t, err)
	assert.Equal(t, int64(27), page.Total)
	require.Len(t, page.Orgs, 2)
	assert.Equal(t, int64(90), page.Orgs[0].Clicks30d)
	assert.Equal(t, int64(1), page.Orgs[0].Domains)
	// An org with no grouped rows keeps zero counts.
	assert.Zero(t, page.Orgs[1].Members)
	assert.Zero(t, page.Orgs[1].Clicks30d)
}

func TestAdminService_Reports_PagesNewestFirst(t *testing.T) {
	svc, store, _, ctx := newAdminService(t)

	store.EXPECT().ListReports(ctx, adminPageSize, adminPageSize*2).Return([]models.AdminReport{
		{ID: 6, Code: "abc1234", LinkStatus: "DISABLED_ABUSE"},
	}, nil)
	store.EXPECT().CountReports(ctx).Return(int64(51), nil)

	page, err := svc.Reports(ctx, 3)
	require.NoError(t, err)
	assert.Equal(t, int64(51), page.Total)
	require.Len(t, page.Reports, 1)
	assert.Equal(t, "DISABLED_ABUSE", page.Reports[0].LinkStatus)
}

func adminLink(id int64, status string) *models.AdminLinkDetail {
	return &models.AdminLinkDetail{ID: id, OrgID: 3, Code: "abc1234", Status: status, OrgName: "Acme"}
}

func TestAdminService_Link_UnknownIDIs404(t *testing.T) {
	svc, store, _, ctx := newAdminService(t)
	store.EXPECT().GetLink(ctx, int64(99)).Return(nil, nil)

	link, err := svc.Link(ctx, 99)
	require.Error(t, err)
	assert.Nil(t, link)
	assertStatus(t, err, http.StatusNotFound)
}

func TestAdminService_DisableEnable_RoundTrip(t *testing.T) {
	svc, store, links, ctx := newAdminService(t)

	// Disable flips to DISABLED_ABUSE via the same SetStatusByID write the
	// re-scan uses.
	store.EXPECT().GetLink(ctx, int64(9)).Return(adminLink(9, models.LinkStatusActive), nil)
	links.EXPECT().SetStatusByID(ctx, int64(9), models.LinkStatusDisabledAbuse).Return(nil)

	disabled, err := svc.DisableLink(ctx, 9, "operator triage")
	require.NoError(t, err)
	assert.Equal(t, models.LinkStatusDisabledAbuse, disabled.Status)

	// Enable flips back to ACTIVE.
	store.EXPECT().GetLink(ctx, int64(9)).Return(adminLink(9, models.LinkStatusDisabledAbuse), nil)
	links.EXPECT().SetStatusByID(ctx, int64(9), models.LinkStatusActive).Return(nil)

	enabled, err := svc.EnableLink(ctx, 9)
	require.NoError(t, err)
	assert.Equal(t, models.LinkStatusActive, enabled.Status)
}

func TestAdminService_DisableLink_UnknownIDIs404(t *testing.T) {
	svc, store, _, ctx := newAdminService(t)
	store.EXPECT().GetLink(ctx, int64(99)).Return(nil, nil)

	link, err := svc.DisableLink(ctx, 99, "")
	require.Error(t, err)
	assert.Nil(t, link)
	assertStatus(t, err, http.StatusNotFound)
}

func TestAdminService_DisableLink_WriteError(t *testing.T) {
	svc, store, links, ctx := newAdminService(t)
	store.EXPECT().GetLink(ctx, int64(9)).Return(adminLink(9, models.LinkStatusActive), nil)
	links.EXPECT().SetStatusByID(ctx, int64(9), models.LinkStatusDisabledAbuse).Return(errBoom)

	link, err := svc.DisableLink(ctx, 9, "")
	require.Error(t, err)
	assert.Nil(t, link)
}

func TestAdminService_DailyStats_ZeroFillsWindow(t *testing.T) {
	svc, store, _, ctx := newAdminService(t)

	fixedAdminNow(t, "2026-07-22T10:00:00Z")

	store.EXPECT().SignupsPerDay(ctx, "2026-07-20").
		Return([]models.DayCount{{Date: "2026-07-21", Clicks: 4}}, nil)
	store.EXPECT().LinksCreatedPerDay(ctx, "2026-07-20").
		Return([]models.DayCount{{Date: "2026-07-20", Clicks: 2}}, nil)
	store.EXPECT().ClicksPerDay(ctx, "2026-07-20").
		Return([]models.DayCount{{Date: "2026-07-22", Clicks: 9}}, nil)

	stats, err := svc.DailyStats(ctx, 3)
	require.NoError(t, err)
	require.Len(t, stats.Days, 3)

	assert.Equal(t, models.AdminDayStat{Date: "2026-07-20", LinksCreated: 2}, stats.Days[0])
	assert.Equal(t, models.AdminDayStat{Date: "2026-07-21", Signups: 4}, stats.Days[1])
	assert.Equal(t, models.AdminDayStat{Date: "2026-07-22", Clicks: 9}, stats.Days[2])
}

func TestAdminService_DailyStats_Bounds(t *testing.T) {
	tests := []struct {
		desc      string
		days      int
		wantSince string
		wantLen   int
	}{
		{desc: "zero defaults to 30", days: 0, wantSince: "2026-06-23", wantLen: 30},
		{desc: "over the cap clamps to 90", days: 365, wantSince: "2026-04-24", wantLen: 90},
		{desc: "single day", days: 1, wantSince: "2026-07-22", wantLen: 1},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			svc, store, _, ctx := newAdminService(t)

			fixedAdminNow(t, "2026-07-22T10:00:00Z")

			store.EXPECT().SignupsPerDay(ctx, tc.wantSince).Return([]models.DayCount{}, nil)
			store.EXPECT().LinksCreatedPerDay(ctx, tc.wantSince).Return([]models.DayCount{}, nil)
			store.EXPECT().ClicksPerDay(ctx, tc.wantSince).Return([]models.DayCount{}, nil)

			stats, err := svc.DailyStats(ctx, tc.days)
			require.NoError(t, err)
			assert.Len(t, stats.Days, tc.wantLen)
			assert.Equal(t, tc.wantSince, stats.Days[0].Date)
			assert.Equal(t, "2026-07-22", stats.Days[len(stats.Days)-1].Date)
		})
	}
}

func TestAdminService_DailyStats_StoreError(t *testing.T) {
	svc, store, _, ctx := newAdminService(t)

	fixedAdminNow(t, "2026-07-22T10:00:00Z")

	store.EXPECT().SignupsPerDay(ctx, gomock.Any()).Return(nil, errBoom)

	stats, err := svc.DailyStats(ctx, 7)
	require.Error(t, err)
	assert.Nil(t, stats)
}
