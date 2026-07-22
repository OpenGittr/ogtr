package stores

import (
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/opengittr/ogtr/backend/models"
)

func TestAdminStore_LikePattern(t *testing.T) {
	tests := []struct {
		desc, term, want string
	}{
		{desc: "empty term matches everything", term: "", want: "%%"},
		{desc: "plain term wrapped", term: "acme", want: "%acme%"},
		{desc: "percent escaped", term: "50%", want: `%50\%%`},
		{desc: "underscore escaped", term: "a_b", want: `%a\_b%`},
		{desc: "backslash escaped", term: `a\b`, want: `%a\\b%`},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			assert.Equal(t, tc.want, likePattern(tc.term))
		})
	}
}

func TestAdminStore_ListUsers(t *testing.T) {
	ctx, mocks := newTestCtx(t)

	rows := sqlmock.NewRows([]string{"id", "email", "name", "created_at"}).
		AddRow(2, "b@x.co", "Bee", time.Now()).
		AddRow(1, "a@x.co", "Aye", time.Now())
	mocks.SQL.ExpectQuery(listUsersQuery).WithArgs("%x.co%", "%x.co%", 25, 0).WillReturnRows(rows)

	users, err := NewAdminStore().ListUsers(ctx, "x.co", 25, 0)
	require.NoError(t, err)
	require.Len(t, users, 2)
	assert.Equal(t, int64(2), users[0].ID)
	assert.Equal(t, "b@x.co", users[0].Email)
	assert.NotNil(t, users[0].Orgs, "orgs must marshal as [] even before memberships attach")
}

func TestAdminStore_ListUsers_Error(t *testing.T) {
	ctx, mocks := newTestCtx(t)
	mocks.SQL.ExpectQuery(listUsersQuery).WillReturnError(errDB)

	users, err := NewAdminStore().ListUsers(ctx, "", 25, 0)
	require.Error(t, err)
	assert.Nil(t, users)
}

func TestAdminStore_CountUsers(t *testing.T) {
	ctx, mocks := newTestCtx(t)
	mocks.SQL.ExpectQuery(countUsersQuery).WithArgs("%%", "%%").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(41))

	n, err := NewAdminStore().CountUsers(ctx, "")
	require.NoError(t, err)
	assert.Equal(t, int64(41), n)
}

func TestAdminStore_UserOrgs(t *testing.T) {
	ctx, mocks := newTestCtx(t)

	rows := sqlmock.NewRows([]string{"user_id", "id", "name", "role"}).
		AddRow(1, 10, "Acme", "OWNER").
		AddRow(1, 11, "Beta", "MEMBER").
		AddRow(2, 10, "Acme", "MEMBER")
	mocks.SQL.ExpectQuery("SELECT om.user_id, o.id, o.name, om.role FROM org_members om "+
		"INNER JOIN orgs o ON o.id = om.org_id WHERE om.user_id IN (?, ?) ORDER BY om.user_id, o.id").
		WithArgs(int64(1), int64(2)).WillReturnRows(rows)

	memberships, err := NewAdminStore().UserOrgs(ctx, []int64{1, 2})
	require.NoError(t, err)
	require.Len(t, memberships[1], 2, "multi-org user carries both orgs")
	assert.Equal(t, "OWNER", memberships[1][0].Role)
	assert.Equal(t, "Beta", memberships[1][1].Name)
	require.Len(t, memberships[2], 1)
}

func TestAdminStore_UserOrgs_EmptyIDsSkipsQuery(t *testing.T) {
	ctx, _ := newTestCtx(t)

	memberships, err := NewAdminStore().UserOrgs(ctx, nil)
	require.NoError(t, err)
	assert.Empty(t, memberships)
}

func TestAdminStore_ListOrgs(t *testing.T) {
	ctx, mocks := newTestCtx(t)

	rows := sqlmock.NewRows([]string{"id", "name", "slug", "created_at"}).
		AddRow(10, "Acme", "acme", time.Now())
	mocks.SQL.ExpectQuery(listOrgsQuery).WithArgs("%ac%", "%ac%", 25, 25).WillReturnRows(rows)

	orgs, err := NewAdminStore().ListOrgs(ctx, "ac", 25, 25)
	require.NoError(t, err)
	require.Len(t, orgs, 1)
	assert.Equal(t, "acme", orgs[0].Slug)
	assert.Zero(t, orgs[0].Members, "counts are filled by the service, not the listing")
}

func TestAdminStore_OrgCounts(t *testing.T) {
	ctx, mocks := newTestCtx(t)

	grouped := func(pairs ...[2]int64) *sqlmock.Rows {
		rows := sqlmock.NewRows([]string{"org_id", "count"})
		for _, p := range pairs {
			rows.AddRow(p[0], p[1])
		}

		return rows
	}

	mocks.SQL.ExpectQuery("SELECT org_id, COUNT(*) FROM org_members WHERE org_id IN (?, ?) GROUP BY org_id").
		WithArgs(int64(10), int64(11)).WillReturnRows(grouped([2]int64{10, 3}, [2]int64{11, 1}))
	mocks.SQL.ExpectQuery("SELECT org_id, COUNT(id) FROM links WHERE org_id IN (?, ?) GROUP BY org_id").
		WithArgs(int64(10), int64(11)).WillReturnRows(grouped([2]int64{10, 7}))
	mocks.SQL.ExpectQuery("SELECT org_id, COUNT(id) FROM clicks WHERE org_id IN (?, ?) AND ts >= ? GROUP BY org_id").
		WithArgs(int64(10), int64(11), "2026-06-22 00:00:00").WillReturnRows(grouped([2]int64{10, 90}))
	mocks.SQL.ExpectQuery("SELECT org_id, COUNT(id) FROM domains WHERE org_id IN (?, ?) GROUP BY org_id").
		WithArgs(int64(10), int64(11)).WillReturnRows(grouped([2]int64{11, 2}))

	counts, err := NewAdminStore().OrgCounts(ctx, []int64{10, 11}, "2026-06-22 00:00:00")
	require.NoError(t, err)
	assert.Equal(t, models.AdminOrgCounts{Members: 3, Links: 7, Clicks30d: 90}, counts[10])
	assert.Equal(t, models.AdminOrgCounts{Members: 1, Domains: 2}, counts[11])
}

func TestAdminStore_OrgCounts_EmptyIDsSkipsQueries(t *testing.T) {
	ctx, _ := newTestCtx(t)

	counts, err := NewAdminStore().OrgCounts(ctx, nil, "2026-06-22 00:00:00")
	require.NoError(t, err)
	assert.Empty(t, counts)
}

func TestAdminStore_ListReports(t *testing.T) {
	ctx, mocks := newTestCtx(t)

	contact := "victim@mail.co"
	rows := sqlmock.NewRows([]string{"id", "code", "link_id", "org_id", "reason", "reporter_contact",
		"created_at", "status", "destination_url"}).
		AddRow(5, "abc1234", 9, 3, "phishing", &contact, time.Now(), "ACTIVE", "https://evil.example")
	mocks.SQL.ExpectQuery(listReportsQuery).WithArgs(25, 0).WillReturnRows(rows)

	reports, err := NewAdminStore().ListReports(ctx, 25, 0)
	require.NoError(t, err)
	require.Len(t, reports, 1)
	assert.Equal(t, "ACTIVE", reports[0].LinkStatus)
	assert.Equal(t, "https://evil.example", reports[0].DestinationURL)
	require.NotNil(t, reports[0].ReporterContact)
	assert.Equal(t, contact, *reports[0].ReporterContact)
}

func TestAdminStore_CountReports(t *testing.T) {
	ctx, mocks := newTestCtx(t)
	mocks.SQL.ExpectQuery(countReportsQuery).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(6))

	n, err := NewAdminStore().CountReports(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(6), n)
}

func TestAdminStore_GetLink(t *testing.T) {
	ctx, mocks := newTestCtx(t)

	email := "maker@acme.com"
	rows := sqlmock.NewRows([]string{"id", "org_id", "code", "destination_url", "status",
		"created_at", "visits", "name", "email"}).
		AddRow(9, 3, "abc1234", "https://x.co", "ACTIVE", time.Now(), 12, "Acme", &email)
	mocks.SQL.ExpectQuery(adminLinkQuery).WithArgs(int64(9)).WillReturnRows(rows)

	link, err := NewAdminStore().GetLink(ctx, 9)
	require.NoError(t, err)
	require.NotNil(t, link)
	assert.Equal(t, "Acme", link.OrgName)
	require.NotNil(t, link.CreatorEmail)
	assert.Equal(t, email, *link.CreatorEmail)
}

func TestAdminStore_GetLink_APIKeyLinkHasNoCreator(t *testing.T) {
	ctx, mocks := newTestCtx(t)

	rows := sqlmock.NewRows([]string{"id", "org_id", "code", "destination_url", "status",
		"created_at", "visits", "name", "email"}).
		AddRow(9, 3, "abc1234", "https://x.co", "ACTIVE", time.Now(), 12, "Acme", nil)
	mocks.SQL.ExpectQuery(adminLinkQuery).WithArgs(int64(9)).WillReturnRows(rows)

	link, err := NewAdminStore().GetLink(ctx, 9)
	require.NoError(t, err)
	require.NotNil(t, link)
	assert.Nil(t, link.CreatorEmail)
}

func TestAdminStore_GetLink_NotFound(t *testing.T) {
	ctx, mocks := newTestCtx(t)
	mocks.SQL.ExpectQuery(adminLinkQuery).WithArgs(int64(99)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "org_id", "code", "destination_url", "status",
			"created_at", "visits", "name", "email"}))

	link, err := NewAdminStore().GetLink(ctx, 99)
	require.NoError(t, err)
	assert.Nil(t, link)
}

func TestAdminStore_PerDaySeries(t *testing.T) {
	store := NewAdminStore()

	t.Run("signups per day", func(t *testing.T) {
		ctx, mocks := newTestCtx(t)
		mocks.SQL.ExpectQuery(signupsPerDayQuery).WithArgs("2026-06-23").
			WillReturnRows(sqlmock.NewRows([]string{"date", "count"}).AddRow("2026-06-23", 4))

		series, err := store.SignupsPerDay(ctx, "2026-06-23")
		require.NoError(t, err)
		require.Len(t, series, 1)
		assert.Equal(t, int64(4), series[0].Clicks)
	})

	t.Run("links created per day", func(t *testing.T) {
		ctx, mocks := newTestCtx(t)
		mocks.SQL.ExpectQuery(linksCreatedPerDayQuery).WithArgs("2026-06-23").
			WillReturnRows(sqlmock.NewRows([]string{"date", "count"}).AddRow("2026-06-24", 2))

		series, err := store.LinksCreatedPerDay(ctx, "2026-06-23")
		require.NoError(t, err)
		require.Len(t, series, 1)
		assert.Equal(t, "2026-06-24", series[0].Date)
	})

	t.Run("clicks per day", func(t *testing.T) {
		ctx, mocks := newTestCtx(t)
		mocks.SQL.ExpectQuery(clicksPerDayAdminQuery).WithArgs("2026-06-23").
			WillReturnRows(sqlmock.NewRows([]string{"date", "count"}).AddRow("2026-06-25", 9))

		series, err := store.ClicksPerDay(ctx, "2026-06-23")
		require.NoError(t, err)
		require.Len(t, series, 1)
		assert.Equal(t, int64(9), series[0].Clicks)
	})
}
