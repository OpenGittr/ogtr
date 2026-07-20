package stores

import (
	"database/sql/driver"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gofr.dev/pkg/gofr"

	"github.com/opengittr/ogtr/backend/models"
)

const (
	statsOrg  = int64(3)
	statsLink = int64(9)
	statsFrom = "2026-06-14"
	statsTo   = "2026-07-14"
)

func statsArgs() []driver.Value { return []driver.Value{statsOrg, statsLink, statsFrom, statsTo} }

func TestStatsStore_TotalClicks(t *testing.T) {
	tests := []struct {
		desc    string
		mock    func(m sqlmock.Sqlmock)
		want    int64
		wantErr bool
	}{
		{
			desc: "counted",
			mock: func(m sqlmock.Sqlmock) {
				m.ExpectQuery(totalClicksQuery).WithArgs(statsArgs()...).
					WillReturnRows(sqlmock.NewRows([]string{"n"}).AddRow(30))
			},
			want: 30,
		},
		{
			desc: "db error",
			mock: func(m sqlmock.Sqlmock) {
				m.ExpectQuery(totalClicksQuery).WithArgs(statsArgs()...).WillReturnError(errDB)
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			ctx, mocks := newTestCtx(t)
			tc.mock(mocks.SQL)

			got, err := NewStatsStore().TotalClicks(ctx, statsOrg, statsLink, statsFrom, statsTo)

			assert.Equal(t, tc.wantErr, err != nil)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestStatsStore_ClicksPerDay(t *testing.T) {
	tests := []struct {
		desc    string
		mock    func(m sqlmock.Sqlmock)
		want    []models.DayCount
		wantErr bool
	}{
		{
			desc: "series returned in day order",
			mock: func(m sqlmock.Sqlmock) {
				m.ExpectQuery(clicksPerDayQuery).WithArgs(statsArgs()...).
					WillReturnRows(sqlmock.NewRows([]string{"date", "n"}).
						AddRow("2026-07-13", 12).AddRow("2026-07-14", 18))
			},
			want: []models.DayCount{
				{Date: "2026-07-13", Clicks: 12},
				{Date: "2026-07-14", Clicks: 18},
			},
		},
		{
			desc: "no clicks is an empty (non-nil) series",
			mock: func(m sqlmock.Sqlmock) {
				m.ExpectQuery(clicksPerDayQuery).WithArgs(statsArgs()...).
					WillReturnRows(sqlmock.NewRows([]string{"date", "n"}))
			},
			want: []models.DayCount{},
		},
		{
			desc: "db error",
			mock: func(m sqlmock.Sqlmock) {
				m.ExpectQuery(clicksPerDayQuery).WithArgs(statsArgs()...).WillReturnError(errDB)
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			ctx, mocks := newTestCtx(t)
			tc.mock(mocks.SQL)

			got, err := NewStatsStore().ClicksPerDay(ctx, statsOrg, statsLink, statsFrom, statsTo)

			assert.Equal(t, tc.wantErr, err != nil)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestStatsStore_PerDayBreakdowns drives all four per-day dimension methods
// against their exact SQL — the query constants pin org/link/range scoping
// and the mobile_os 'NA' exclusion structurally.
func TestStatsStore_PerDayBreakdowns(t *testing.T) {
	store := NewStatsStore()

	tests := []struct {
		desc  string
		query string
		call  func(ctx *gofr.Context) ([]models.DayDimCount, error)
	}{
		{"browser", browserPerDayQuery, func(ctx *gofr.Context) ([]models.DayDimCount, error) {
			return store.BrowserPerDay(ctx, statsOrg, statsLink, statsFrom, statsTo)
		}},
		{"device_type", devicePerDayQuery, func(ctx *gofr.Context) ([]models.DayDimCount, error) {
			return store.DevicePerDay(ctx, statsOrg, statsLink, statsFrom, statsTo)
		}},
		{"referrer", referrerPerDayQuery, func(ctx *gofr.Context) ([]models.DayDimCount, error) {
			return store.ReferrerPerDay(ctx, statsOrg, statsLink, statsFrom, statsTo)
		}},
		{"mobile_os", mobileOSPerDayQuery, func(ctx *gofr.Context) ([]models.DayDimCount, error) {
			return store.MobileOSPerDay(ctx, statsOrg, statsLink, statsFrom, statsTo)
		}},
		{"country", countryPerDayQuery, func(ctx *gofr.Context) ([]models.DayDimCount, error) {
			return store.CountryPerDay(ctx, statsOrg, statsLink, statsFrom, statsTo)
		}},
		{"region", regionPerDayQuery, func(ctx *gofr.Context) ([]models.DayDimCount, error) {
			return store.RegionPerDay(ctx, statsOrg, statsLink, statsFrom, statsTo)
		}},
		{"city", cityPerDayQuery, func(ctx *gofr.Context) ([]models.DayDimCount, error) {
			return store.CityPerDay(ctx, statsOrg, statsLink, statsFrom, statsTo)
		}},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			ctx, mocks := newTestCtx(t)
			mocks.SQL.ExpectQuery(tc.query).WithArgs(statsArgs()...).
				WillReturnRows(sqlmock.NewRows([]string{"date", "value", "n"}).
					AddRow("2026-07-13", "A", 2).AddRow("2026-07-14", "B", 3))

			got, err := tc.call(ctx)

			require.NoError(t, err)
			assert.Equal(t, []models.DayDimCount{
				{Date: "2026-07-13", Value: "A", Clicks: 2},
				{Date: "2026-07-14", Value: "B", Clicks: 3},
			}, got)
		})

		t.Run(tc.desc+" db error", func(t *testing.T) {
			ctx, mocks := newTestCtx(t)
			mocks.SQL.ExpectQuery(tc.query).WithArgs(statsArgs()...).WillReturnError(errDB)

			_, err := tc.call(ctx)

			require.Error(t, err)
		})
	}
}

// TestStatsStore_TotalBreakdowns drives all four whole-range dimension totals.
func TestStatsStore_TotalBreakdowns(t *testing.T) {
	store := NewStatsStore()

	tests := []struct {
		desc  string
		query string
		call  func(ctx *gofr.Context) ([]models.DimCount, error)
	}{
		{"browser", browserTotalsQuery, func(ctx *gofr.Context) ([]models.DimCount, error) {
			return store.BrowserTotals(ctx, statsOrg, statsLink, statsFrom, statsTo)
		}},
		{"device_type", deviceTotalsQuery, func(ctx *gofr.Context) ([]models.DimCount, error) {
			return store.DeviceTotals(ctx, statsOrg, statsLink, statsFrom, statsTo)
		}},
		{"referrer", referrerTotalsQuery, func(ctx *gofr.Context) ([]models.DimCount, error) {
			return store.ReferrerTotals(ctx, statsOrg, statsLink, statsFrom, statsTo)
		}},
		{"mobile_os", mobileOSTotalsQuery, func(ctx *gofr.Context) ([]models.DimCount, error) {
			return store.MobileOSTotals(ctx, statsOrg, statsLink, statsFrom, statsTo)
		}},
		{"country", countryTotalsQuery, func(ctx *gofr.Context) ([]models.DimCount, error) {
			return store.CountryTotals(ctx, statsOrg, statsLink, statsFrom, statsTo)
		}},
		{"region", regionTotalsQuery, func(ctx *gofr.Context) ([]models.DimCount, error) {
			return store.RegionTotals(ctx, statsOrg, statsLink, statsFrom, statsTo)
		}},
		{"city", cityTotalsQuery, func(ctx *gofr.Context) ([]models.DimCount, error) {
			return store.CityTotals(ctx, statsOrg, statsLink, statsFrom, statsTo)
		}},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			ctx, mocks := newTestCtx(t)
			mocks.SQL.ExpectQuery(tc.query).WithArgs(statsArgs()...).
				WillReturnRows(sqlmock.NewRows([]string{"value", "n"}).
					AddRow("A", 20).AddRow("B", 10))

			got, err := tc.call(ctx)

			require.NoError(t, err)
			assert.Equal(t, []models.DimCount{{Value: "A", Clicks: 20}, {Value: "B", Clicks: 10}}, got)
		})

		t.Run(tc.desc+" db error", func(t *testing.T) {
			ctx, mocks := newTestCtx(t)
			mocks.SQL.ExpectQuery(tc.query).WithArgs(statsArgs()...).WillReturnError(errDB)

			_, err := tc.call(ctx)

			require.Error(t, err)
		})
	}
}

// TestStatsStore_LocationQueries_UnknownLabeling pins the SQL shape of the
// six location queries: NULL geo values label as 'Unknown' (selected value
// AND tie-break ordering both use the COALESCEd expression) while grouping
// stays on the raw column (only_full_group_by-safe).
func TestStatsStore_LocationQueries_UnknownLabeling(t *testing.T) {
	tests := []struct {
		desc   string
		query  string
		column string
	}{
		{"country totals", countryTotalsQuery, "country"},
		{"region totals", regionTotalsQuery, "region"},
		{"city totals", cityTotalsQuery, "city"},
		{"country per day", countryPerDayQuery, "country"},
		{"region per day", regionPerDayQuery, "region"},
		{"city per day", cityPerDayQuery, "city"},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			label := "COALESCE(" + tc.column + ", 'Unknown')"

			assert.Contains(t, tc.query, label, "NULL geo must roll up as 'Unknown'")
			assert.Equal(t, 2, strings.Count(tc.query, label),
				"the COALESCEd label must appear in SELECT and ORDER BY")
			assert.Contains(t, tc.query, rangeFilter, "org/link/range scoping must match the other breakdowns")

			groupBy := tc.query[strings.Index(tc.query, "GROUP BY"):strings.Index(tc.query, "ORDER BY")]
			assert.Contains(t, groupBy, tc.column)
			assert.NotContains(t, groupBy, "COALESCE",
				"GROUP BY must use the raw column, not the COALESCE (only_full_group_by)")
		})
	}
}

func TestStatsStore_DeeplinkClickCount(t *testing.T) {
	ctx, mocks := newTestCtx(t)
	mocks.SQL.ExpectQuery(deeplinkCountQuery).
		WithArgs(statsOrg, statsLink, statsFrom, statsTo, true).
		WillReturnRows(sqlmock.NewRows([]string{"n"}).AddRow(4))

	got, err := NewStatsStore().DeeplinkClickCount(ctx, statsOrg, statsLink, statsFrom, statsTo, true)

	require.NoError(t, err)
	assert.Equal(t, int64(4), got)
}

func TestStatsStore_TargetMatchedCount(t *testing.T) {
	ctx, mocks := newTestCtx(t)
	mocks.SQL.ExpectQuery(targetMatchedQuery).WithArgs(statsArgs()...).
		WillReturnRows(sqlmock.NewRows([]string{"n"}).AddRow(6))

	got, err := NewStatsStore().TargetMatchedCount(ctx, statsOrg, statsLink, statsFrom, statsTo)

	require.NoError(t, err)
	assert.Equal(t, int64(6), got)
}

func TestStatsStore_ClickDetails(t *testing.T) {
	ts := time.Date(2026, 7, 14, 10, 0, 0, 0, time.UTC)
	tag := "tag-1"

	tests := []struct {
		desc    string
		mock    func(m sqlmock.Sqlmock)
		want    []models.ClickDetail
		wantErr bool
	}{
		{
			desc: "details returned newest first, null tag preserved",
			mock: func(m sqlmock.Sqlmock) {
				m.ExpectQuery(clickDetailsQuery).WithArgs(statsArgs()...).
					WillReturnRows(sqlmock.NewRows([]string{"id", "link_id", "custom_tag_id", "ts"}).
						AddRow(2, statsLink, tag, ts).AddRow(1, statsLink, nil, ts))
			},
			want: []models.ClickDetail{
				{ID: 2, LinkID: statsLink, CustomTagID: &tag, Ts: ts},
				{ID: 1, LinkID: statsLink, CustomTagID: nil, Ts: ts},
			},
		},
		{
			desc: "db error",
			mock: func(m sqlmock.Sqlmock) {
				m.ExpectQuery(clickDetailsQuery).WithArgs(statsArgs()...).WillReturnError(errDB)
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			ctx, mocks := newTestCtx(t)
			tc.mock(mocks.SQL)

			got, err := NewStatsStore().ClickDetails(ctx, statsOrg, statsLink, statsFrom, statsTo)

			assert.Equal(t, tc.wantErr, err != nil)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestStatsStore_UniqueTagClicks(t *testing.T) {
	// The trailing ts bound is the analytics-window retention cutoff
	// (1970-01-01 = unbounded), always bound so the query shape is constant.
	const query = "SELECT COUNT(DISTINCT custom_tag_id) FROM clicks WHERE org_id = ? AND link_id IN (?, ?, ?) AND ts >= ?"

	tests := []struct {
		desc    string
		since   string
		mock    func(m sqlmock.Sqlmock)
		want    int64
		wantErr bool
	}{
		{
			desc:  "distinct tags counted with one placeholder per id",
			since: "1970-01-01",
			mock: func(m sqlmock.Sqlmock) {
				m.ExpectQuery(query).WithArgs(statsOrg, int64(1), int64(2), int64(3), "1970-01-01").
					WillReturnRows(sqlmock.NewRows([]string{"n"}).AddRow(2))
			},
			want: 2,
		},
		{
			desc:  "retention cutoff binds as the ts bound",
			since: "2026-06-21",
			mock: func(m sqlmock.Sqlmock) {
				m.ExpectQuery(query).WithArgs(statsOrg, int64(1), int64(2), int64(3), "2026-06-21").
					WillReturnRows(sqlmock.NewRows([]string{"n"}).AddRow(1))
			},
			want: 1,
		},
		{
			desc:  "db error",
			since: "1970-01-01",
			mock: func(m sqlmock.Sqlmock) {
				m.ExpectQuery(query).WithArgs(statsOrg, int64(1), int64(2), int64(3), "1970-01-01").
					WillReturnError(errDB)
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			ctx, mocks := newTestCtx(t)
			tc.mock(mocks.SQL)

			got, err := NewStatsStore().UniqueTagClicks(ctx, statsOrg, []int64{1, 2, 3}, tc.since)

			assert.Equal(t, tc.wantErr, err != nil)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestStatsStore_DistinctTags(t *testing.T) {
	tests := []struct {
		desc    string
		mock    func(m sqlmock.Sqlmock)
		want    []string
		wantErr bool
	}{
		{
			desc: "full set in one query",
			mock: func(m sqlmock.Sqlmock) {
				m.ExpectQuery(distinctTagsQuery).WithArgs(statsOrg, "1970-01-01").
					WillReturnRows(sqlmock.NewRows([]string{"tag"}).AddRow("promo").AddRow("social"))
			},
			want: []string{"promo", "social"},
		},
		{
			desc: "no tags is an empty (non-nil) list",
			mock: func(m sqlmock.Sqlmock) {
				m.ExpectQuery(distinctTagsQuery).WithArgs(statsOrg, "1970-01-01").
					WillReturnRows(sqlmock.NewRows([]string{"tag"}))
			},
			want: []string{},
		},
		{
			desc: "db error",
			mock: func(m sqlmock.Sqlmock) {
				m.ExpectQuery(distinctTagsQuery).WithArgs(statsOrg, "1970-01-01").WillReturnError(errDB)
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			ctx, mocks := newTestCtx(t)
			tc.mock(mocks.SQL)

			got, err := NewStatsStore().DistinctTags(ctx, statsOrg, "1970-01-01")

			assert.Equal(t, tc.wantErr, err != nil)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestStatsStore_UTMCounts drives the three UTM analyses against their exact
// SQL — the constants pin the org + visibility scoping and empty-value skips.
func TestStatsStore_UTMCounts(t *testing.T) {
	store := NewStatsStore()
	viewer := int64(7)

	tests := []struct {
		desc  string
		query string
		call  func(ctx *gofr.Context) ([]models.UTMCount, error)
	}{
		{"source", utmSourceQuery, func(ctx *gofr.Context) ([]models.UTMCount, error) {
			return store.UTMSourceCounts(ctx, statsOrg, viewer, "1970-01-01")
		}},
		{"medium", utmMediumQuery, func(ctx *gofr.Context) ([]models.UTMCount, error) {
			return store.UTMMediumCounts(ctx, statsOrg, viewer, "1970-01-01")
		}},
		{"campaign", utmCampaignQuery, func(ctx *gofr.Context) ([]models.UTMCount, error) {
			return store.UTMCampaignCounts(ctx, statsOrg, viewer, "1970-01-01")
		}},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			ctx, mocks := newTestCtx(t)
			mocks.SQL.ExpectQuery(tc.query).WithArgs(statsOrg, viewer, "1970-01-01").
				WillReturnRows(sqlmock.NewRows([]string{"value", "id", "code", "url", "n"}).
					AddRow("google", 9, "abc1234", "https://x.co", 5))

			got, err := tc.call(ctx)

			require.NoError(t, err)
			assert.Equal(t, []models.UTMCount{
				{UTMValue: "google", LinkID: 9, Code: "abc1234", URL: "https://x.co", Clicks: 5},
			}, got)
		})

		t.Run(tc.desc+" db error", func(t *testing.T) {
			ctx, mocks := newTestCtx(t)
			mocks.SQL.ExpectQuery(tc.query).WithArgs(statsOrg, viewer, "1970-01-01").WillReturnError(errDB)

			_, err := tc.call(ctx)

			require.Error(t, err)
		})
	}
}
