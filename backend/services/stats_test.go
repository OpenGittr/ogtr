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

const (
	statsFrom = "2026-06-01"
	statsTo   = "2026-06-30"
)

var errStatsDB = errors.New("stats db down")

func newStatsService(t *testing.T) (*StatsService, *MockStatsStore, *MockLinkStore, *MockRuleStore, *gofr.Context) {
	t.Helper()

	ctrl := gomock.NewController(t)
	stats := NewMockStatsStore(ctrl)
	links := NewMockLinkStore(ctrl)
	rules := NewMockRuleStore(ctrl)

	mockContainer, _ := container.NewMockContainer(t)
	ctx := &gofr.Context{Context: context.Background(), Container: mockContainer}

	return NewStatsService(stats, links, rules), stats, links, rules, ctx
}

// expectCoreStats wires the sixteen core report queries with distinct values
// so the assembled report proves each store result lands in the right field.
func expectCoreStats(stats *MockStatsStore) {
	stats.EXPECT().TotalClicks(gomock.Any(), int64(3), int64(9), statsFrom, statsTo).Return(int64(30), nil)
	stats.EXPECT().ClicksPerDay(gomock.Any(), int64(3), int64(9), statsFrom, statsTo).
		Return([]models.DayCount{{Date: "2026-06-14", Clicks: 30}}, nil)

	perDay := func(v string) []models.DayDimCount {
		return []models.DayDimCount{{Date: "2026-06-14", Value: v, Clicks: 1}}
	}
	stats.EXPECT().BrowserPerDay(gomock.Any(), int64(3), int64(9), statsFrom, statsTo).Return(perDay("Chrome"), nil)
	stats.EXPECT().DevicePerDay(gomock.Any(), int64(3), int64(9), statsFrom, statsTo).Return(perDay("Mobile"), nil)
	stats.EXPECT().ReferrerPerDay(gomock.Any(), int64(3), int64(9), statsFrom, statsTo).Return(perDay("t.co"), nil)
	stats.EXPECT().MobileOSPerDay(gomock.Any(), int64(3), int64(9), statsFrom, statsTo).Return(perDay("iOS"), nil)
	stats.EXPECT().CountryPerDay(gomock.Any(), int64(3), int64(9), statsFrom, statsTo).Return(perDay("India"), nil)
	stats.EXPECT().RegionPerDay(gomock.Any(), int64(3), int64(9), statsFrom, statsTo).Return(perDay("Karnataka"), nil)
	stats.EXPECT().CityPerDay(gomock.Any(), int64(3), int64(9), statsFrom, statsTo).Return(perDay("Bengaluru"), nil)

	totals := func(v string) []models.DimCount { return []models.DimCount{{Value: v, Clicks: 2}} }
	stats.EXPECT().BrowserTotals(gomock.Any(), int64(3), int64(9), statsFrom, statsTo).Return(totals("Chrome"), nil)
	stats.EXPECT().DeviceTotals(gomock.Any(), int64(3), int64(9), statsFrom, statsTo).Return(totals("Mobile"), nil)
	stats.EXPECT().ReferrerTotals(gomock.Any(), int64(3), int64(9), statsFrom, statsTo).Return(totals("t.co"), nil)
	stats.EXPECT().MobileOSTotals(gomock.Any(), int64(3), int64(9), statsFrom, statsTo).Return(totals("iOS"), nil)
	stats.EXPECT().CountryTotals(gomock.Any(), int64(3), int64(9), statsFrom, statsTo).
		Return([]models.DimCount{{Value: "India", Clicks: 28}, {Value: "Unknown", Clicks: 2}}, nil)
	stats.EXPECT().RegionTotals(gomock.Any(), int64(3), int64(9), statsFrom, statsTo).Return(totals("Karnataka"), nil)
	stats.EXPECT().CityTotals(gomock.Any(), int64(3), int64(9), statsFrom, statsTo).Return(totals("Bengaluru"), nil)
}

func TestStatsService_LinkReport(t *testing.T) {
	svc, stats, links, rules, ctx := newStatsService(t)

	links.EXPECT().GetByID(gomock.Any(), int64(3), int64(9)).
		Return(publicLink(9, "abc1234", "https://x.co"), nil)
	expectCoreStats(stats)
	// deeplink=false: flagged count and the always-true app-open count.
	stats.EXPECT().DeeplinkClickCount(gomock.Any(), int64(3), int64(9), statsFrom, statsTo, false).Return(int64(26), nil)
	stats.EXPECT().DeeplinkClickCount(gomock.Any(), int64(3), int64(9), statsFrom, statsTo, true).Return(int64(4), nil)
	// The link has a rule: the target section is present.
	rules.EXPECT().ListByLink(gomock.Any(), int64(3), int64(9)).Return([]models.Rule{{ID: 1}}, nil)
	stats.EXPECT().TargetMatchedCount(gomock.Any(), int64(3), int64(9), statsFrom, statsTo).Return(int64(6), nil)

	tag := "promo"
	details := []models.ClickDetail{{ID: 42, LinkID: 9, CustomTagID: &tag, Ts: time.Now()}}
	stats.EXPECT().ClickDetails(gomock.Any(), int64(3), int64(9), statsFrom, statsTo).Return(details, nil)

	report, err := svc.LinkReport(ctx, 3, 7, 9, statsFrom, statsTo, false)

	require.NoError(t, err)
	assert.Equal(t, statsFrom, report.From)
	assert.Equal(t, statsTo, report.To)
	assert.Equal(t, int64(30), report.TotalClicks)
	assert.Equal(t, []models.DayCount{{Date: "2026-06-14", Clicks: 30}}, report.ClicksPerDay)
	assert.Equal(t, "Chrome", report.PerDayBreakdowns.Browser[0].Value)
	assert.Equal(t, "Mobile", report.PerDayBreakdowns.DeviceType[0].Value)
	assert.Equal(t, "t.co", report.PerDayBreakdowns.Referrer[0].Value)
	assert.Equal(t, "iOS", report.PerDayBreakdowns.MobileOS[0].Value)
	assert.Equal(t, "India", report.PerDayBreakdowns.Location.Countries[0].Value)
	assert.Equal(t, "Karnataka", report.PerDayBreakdowns.Location.Regions[0].Value)
	assert.Equal(t, "Bengaluru", report.PerDayBreakdowns.Location.Cities[0].Value)
	assert.Equal(t, "Chrome", report.TotalBreakdowns.Browser[0].Value)
	assert.Equal(t, "Mobile", report.TotalBreakdowns.DeviceType[0].Value)
	assert.Equal(t, "t.co", report.TotalBreakdowns.Referrer[0].Value)
	assert.Equal(t, "iOS", report.TotalBreakdowns.MobileOS[0].Value)
	assert.Equal(t, []models.DimCount{{Value: "India", Clicks: 28}, {Value: "Unknown", Clicks: 2}},
		report.TotalBreakdowns.Location.Countries, "Unknown bucket passes through the report")
	assert.Equal(t, "Karnataka", report.TotalBreakdowns.Location.Regions[0].Value)
	assert.Equal(t, "Bengaluru", report.TotalBreakdowns.Location.Cities[0].Value)
	assert.Equal(t, models.DeeplinkStats{DeeplinkClicks: 26, MobileAppOpens: 4}, report.Deeplink)
	require.NotNil(t, report.TargetRule)
	assert.Equal(t, models.TargetRuleStats{TotalClicks: 30, TargetMatched: 6}, *report.TargetRule)
	assert.Equal(t, details, report.Clicks)
}

func TestStatsService_LinkReport_DeeplinkFlagTrue(t *testing.T) {
	svc, stats, links, rules, ctx := newStatsService(t)

	links.EXPECT().GetByID(gomock.Any(), int64(3), int64(9)).
		Return(publicLink(9, "abc1234", "https://x.co"), nil)
	expectCoreStats(stats)
	// deeplink=true: one count serves both fields — no second query.
	stats.EXPECT().DeeplinkClickCount(gomock.Any(), int64(3), int64(9), statsFrom, statsTo, true).Return(int64(4), nil)
	rules.EXPECT().ListByLink(gomock.Any(), int64(3), int64(9)).Return([]models.Rule{}, nil)
	stats.EXPECT().ClickDetails(gomock.Any(), int64(3), int64(9), statsFrom, statsTo).
		Return([]models.ClickDetail{}, nil)

	report, err := svc.LinkReport(ctx, 3, 7, 9, statsFrom, statsTo, true)

	require.NoError(t, err)
	assert.Equal(t, models.DeeplinkStats{DeeplinkClicks: 4, MobileAppOpens: 4}, report.Deeplink)
	// No rules: the target section stays null (FEATURES.md §5.1).
	assert.Nil(t, report.TargetRule)
}

func TestStatsService_LinkReport_Visibility(t *testing.T) {
	private := publicLink(9, "abc1234", "https://x.co")
	private.Type = models.LinkTypePrivate
	private.UserID = ptr64(8) // someone else's PRIVATE link

	tests := []struct {
		desc string
		link *models.Link
	}{
		{desc: "absent link is 404", link: nil},
		{desc: "someone else's PRIVATE link is 404, not 403", link: private},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			svc, _, links, _, ctx := newStatsService(t)
			links.EXPECT().GetByID(gomock.Any(), int64(3), int64(9)).Return(tc.link, nil)

			_, err := svc.LinkReport(ctx, 3, 7, 9, statsFrom, statsTo, false)

			require.Error(t, err)
			assertStatus(t, err, http.StatusNotFound)
		})
	}
}

func TestStatsService_LinkReport_DateValidation(t *testing.T) {
	tests := []struct {
		desc     string
		from, to string
	}{
		{desc: "from after to", from: "2026-07-14", to: "2026-07-01"},
		{desc: "malformed from", from: "14-07-2026", to: ""},
		{desc: "malformed to", from: "", to: "yesterday"},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			svc, _, _, _, ctx := newStatsService(t)

			_, err := svc.LinkReport(ctx, 3, 7, 9, tc.from, tc.to, false)

			require.Error(t, err)
			assertStatus(t, err, http.StatusBadRequest)
		})
	}
}

func TestStatsService_LinkReport_DefaultWindowIsLastMonth(t *testing.T) {
	svc, stats, links, rules, ctx := newStatsService(t)

	wantFrom := time.Now().AddDate(0, -1, 0).Format(statsDateLayout)
	wantTo := time.Now().Format(statsDateLayout)

	links.EXPECT().GetByID(gomock.Any(), int64(3), int64(9)).
		Return(publicLink(9, "abc1234", "https://x.co"), nil)

	stats.EXPECT().TotalClicks(gomock.Any(), int64(3), int64(9), wantFrom, wantTo).Return(int64(0), nil)
	stats.EXPECT().ClicksPerDay(gomock.Any(), int64(3), int64(9), wantFrom, wantTo).Return([]models.DayCount{}, nil)
	stats.EXPECT().BrowserPerDay(gomock.Any(), int64(3), int64(9), wantFrom, wantTo).Return([]models.DayDimCount{}, nil)
	stats.EXPECT().DevicePerDay(gomock.Any(), int64(3), int64(9), wantFrom, wantTo).Return([]models.DayDimCount{}, nil)
	stats.EXPECT().ReferrerPerDay(gomock.Any(), int64(3), int64(9), wantFrom, wantTo).Return([]models.DayDimCount{}, nil)
	stats.EXPECT().MobileOSPerDay(gomock.Any(), int64(3), int64(9), wantFrom, wantTo).Return([]models.DayDimCount{}, nil)
	stats.EXPECT().CountryPerDay(gomock.Any(), int64(3), int64(9), wantFrom, wantTo).Return([]models.DayDimCount{}, nil)
	stats.EXPECT().RegionPerDay(gomock.Any(), int64(3), int64(9), wantFrom, wantTo).Return([]models.DayDimCount{}, nil)
	stats.EXPECT().CityPerDay(gomock.Any(), int64(3), int64(9), wantFrom, wantTo).Return([]models.DayDimCount{}, nil)
	stats.EXPECT().BrowserTotals(gomock.Any(), int64(3), int64(9), wantFrom, wantTo).Return([]models.DimCount{}, nil)
	stats.EXPECT().DeviceTotals(gomock.Any(), int64(3), int64(9), wantFrom, wantTo).Return([]models.DimCount{}, nil)
	stats.EXPECT().ReferrerTotals(gomock.Any(), int64(3), int64(9), wantFrom, wantTo).Return([]models.DimCount{}, nil)
	stats.EXPECT().MobileOSTotals(gomock.Any(), int64(3), int64(9), wantFrom, wantTo).Return([]models.DimCount{}, nil)
	stats.EXPECT().CountryTotals(gomock.Any(), int64(3), int64(9), wantFrom, wantTo).Return([]models.DimCount{}, nil)
	stats.EXPECT().RegionTotals(gomock.Any(), int64(3), int64(9), wantFrom, wantTo).Return([]models.DimCount{}, nil)
	stats.EXPECT().CityTotals(gomock.Any(), int64(3), int64(9), wantFrom, wantTo).Return([]models.DimCount{}, nil)
	stats.EXPECT().DeeplinkClickCount(gomock.Any(), int64(3), int64(9), wantFrom, wantTo, false).Return(int64(0), nil)
	stats.EXPECT().DeeplinkClickCount(gomock.Any(), int64(3), int64(9), wantFrom, wantTo, true).Return(int64(0), nil)
	rules.EXPECT().ListByLink(gomock.Any(), int64(3), int64(9)).Return([]models.Rule{}, nil)
	stats.EXPECT().ClickDetails(gomock.Any(), int64(3), int64(9), wantFrom, wantTo).Return([]models.ClickDetail{}, nil)

	report, err := svc.LinkReport(ctx, 3, 7, 9, "", "", false)

	require.NoError(t, err)
	assert.Equal(t, wantFrom, report.From)
	assert.Equal(t, wantTo, report.To)
}

func TestStatsService_LinkReport_StoreErrors(t *testing.T) {
	t.Run("link lookup error", func(t *testing.T) {
		svc, _, links, _, ctx := newStatsService(t)
		links.EXPECT().GetByID(gomock.Any(), int64(3), int64(9)).Return(nil, errStatsDB)

		_, err := svc.LinkReport(ctx, 3, 7, 9, statsFrom, statsTo, false)

		require.Error(t, err)
	})

	t.Run("core stats error", func(t *testing.T) {
		svc, stats, links, _, ctx := newStatsService(t)
		links.EXPECT().GetByID(gomock.Any(), int64(3), int64(9)).
			Return(publicLink(9, "abc1234", "https://x.co"), nil)
		stats.EXPECT().TotalClicks(gomock.Any(), int64(3), int64(9), statsFrom, statsTo).
			Return(int64(0), errStatsDB)

		_, err := svc.LinkReport(ctx, 3, 7, 9, statsFrom, statsTo, false)

		require.Error(t, err)
	})

	t.Run("rules listing error", func(t *testing.T) {
		svc, stats, links, rules, ctx := newStatsService(t)
		links.EXPECT().GetByID(gomock.Any(), int64(3), int64(9)).
			Return(publicLink(9, "abc1234", "https://x.co"), nil)
		expectCoreStats(stats)
		stats.EXPECT().DeeplinkClickCount(gomock.Any(), int64(3), int64(9), statsFrom, statsTo, false).
			Return(int64(0), nil)
		stats.EXPECT().DeeplinkClickCount(gomock.Any(), int64(3), int64(9), statsFrom, statsTo, true).
			Return(int64(0), nil)
		rules.EXPECT().ListByLink(gomock.Any(), int64(3), int64(9)).Return(nil, errStatsDB)

		_, err := svc.LinkReport(ctx, 3, 7, 9, statsFrom, statsTo, false)

		require.Error(t, err)
	})
}

func TestStatsService_UniqueClicks(t *testing.T) {
	tests := []struct {
		desc       string
		ids        []int64
		setup      func(stats *MockStatsStore)
		want       int64
		wantStatus int
	}{
		{
			desc: "distinct tags counted",
			ids:  []int64{1, 2, 3},
			setup: func(stats *MockStatsStore) {
				stats.EXPECT().UniqueTagClicks(gomock.Any(), int64(3), []int64{1, 2, 3}).Return(int64(2), nil)
			},
			want: 2,
		},
		{desc: "empty id list is 400", ids: nil, wantStatus: http.StatusBadRequest},
		{
			desc:       "oversized id list is 400",
			ids:        make([]int64, maxUniqueClickIDs+1),
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			svc, stats, _, _, ctx := newStatsService(t)
			if tc.setup != nil {
				tc.setup(stats)
			}

			got, err := svc.UniqueClicks(ctx, 3, tc.ids)

			if tc.wantStatus != 0 {
				require.Error(t, err)
				assertStatus(t, err, tc.wantStatus)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.want, got.UniqueClicks)
		})
	}
}

func TestStatsService_Tags(t *testing.T) {
	svc, stats, _, _, ctx := newStatsService(t)
	stats.EXPECT().DistinctTags(gomock.Any(), int64(3)).Return([]string{"promo", "social"}, nil)

	got, err := svc.Tags(ctx, 3)

	require.NoError(t, err)
	assert.Equal(t, []string{"promo", "social"}, got)
}

func TestStatsService_UTMAnalysis(t *testing.T) {
	svc, stats, _, _, ctx := newStatsService(t)

	source := []models.UTMCount{{UTMValue: "google", LinkID: 9, Clicks: 5}}
	medium := []models.UTMCount{{UTMValue: "referrer by Mobile", LinkID: 9, Clicks: 3}}
	campaign := []models.UTMCount{{UTMValue: "launch", LinkID: 9, Clicks: 2}}

	stats.EXPECT().UTMSourceCounts(gomock.Any(), int64(3), int64(7)).Return(source, nil)
	stats.EXPECT().UTMMediumCounts(gomock.Any(), int64(3), int64(7)).Return(medium, nil)
	stats.EXPECT().UTMCampaignCounts(gomock.Any(), int64(3), int64(7)).Return(campaign, nil)

	got, err := svc.UTMAnalysis(ctx, 3, 7)

	require.NoError(t, err)
	assert.Equal(t, source, got.SourceAnalysis)
	assert.Equal(t, medium, got.MediumAnalysis)
	assert.Equal(t, campaign, got.CampaignAnalysis)
}

func TestStatsService_UTMAnalysis_Errors(t *testing.T) {
	tests := []struct {
		desc  string
		setup func(stats *MockStatsStore)
	}{
		{
			desc: "source query fails",
			setup: func(stats *MockStatsStore) {
				stats.EXPECT().UTMSourceCounts(gomock.Any(), int64(3), int64(7)).Return(nil, errStatsDB)
			},
		},
		{
			desc: "medium query fails",
			setup: func(stats *MockStatsStore) {
				stats.EXPECT().UTMSourceCounts(gomock.Any(), int64(3), int64(7)).Return([]models.UTMCount{}, nil)
				stats.EXPECT().UTMMediumCounts(gomock.Any(), int64(3), int64(7)).Return(nil, errStatsDB)
			},
		},
		{
			desc: "campaign query fails",
			setup: func(stats *MockStatsStore) {
				stats.EXPECT().UTMSourceCounts(gomock.Any(), int64(3), int64(7)).Return([]models.UTMCount{}, nil)
				stats.EXPECT().UTMMediumCounts(gomock.Any(), int64(3), int64(7)).Return([]models.UTMCount{}, nil)
				stats.EXPECT().UTMCampaignCounts(gomock.Any(), int64(3), int64(7)).Return(nil, errStatsDB)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			svc, stats, _, _, ctx := newStatsService(t)
			tc.setup(stats)

			got, err := svc.UTMAnalysis(ctx, 3, 7)

			require.Error(t, err)
			assert.Nil(t, got)
		})
	}
}
