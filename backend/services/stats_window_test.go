package services

// Analytics-window tests (limits.Policy AnalyticsWindow): the viewable-events
// gate (Dub-style — viewing 403s, recording never stops) and the retention
// clamp on every stats query. See also resolve_policy_test.go for the
// structural INV-7 guarantee that resolution can never be affected.

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"gofr.dev/pkg/gofr"

	"github.com/opengittr/ogtr/backend/apierrors"
	"github.com/opengittr/ogtr/backend/limits"
	"github.com/opengittr/ogtr/backend/models"
)

// windowPolicy serves a fixed analytics window (or an error); every other
// axis stays allowed via the embedded base.
type windowPolicy struct {
	limits.UnimplementedPolicy

	window limits.Window
	err    error
}

func (p windowPolicy) AnalyticsWindow(*gofr.Context, int64) (limits.Window, error) {
	return p.window, p.err
}

// fixStatsNow pins the stats clock and restores it after the test.
func fixStatsNow(t *testing.T, iso string) {
	t.Helper()

	fixed, err := time.Parse(time.RFC3339, iso)
	require.NoError(t, err)

	prev := statsNow
	statsNow = func() time.Time { return fixed }

	t.Cleanup(func() { statsNow = prev })
}

func TestStatsService_ViewableEventsGate(t *testing.T) {
	tests := []struct {
		desc     string
		window   limits.Window
		events   int64
		wantMsg  string // "" = not gated
		wantCall bool   // DistinctTags reached
	}{
		{
			desc:     "unbounded window never consults usage",
			window:   limits.Window{},
			wantCall: true,
		},
		{
			desc:     "under the bound is allowed",
			window:   limits.Window{ViewableEvents: 100},
			events:   99,
			wantCall: true,
		},
		{
			desc:     "exactly at the bound is still allowed (gate is exceeds, not reaches)",
			window:   limits.Window{ViewableEvents: 100},
			events:   100,
			wantCall: true,
		},
		{
			desc:    "over the bound gates with the default message",
			window:  limits.Window{ViewableEvents: 100},
			events:  101,
			wantMsg: limits.DefaultWindowMessage,
		},
		{
			desc:    "over the bound gates with the policy's message when set",
			window:  limits.Window{ViewableEvents: 100, Message: "events over the viewable bound for this org"},
			events:  101,
			wantMsg: "events over the viewable bound for this org",
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			svc, stats, _, _, usageReader, ctx := newStatsServiceWithPolicy(t, windowPolicy{window: tc.window})

			if tc.window.ViewableEvents > 0 {
				usageReader.EXPECT().EventsThisMonth(gomock.Any(), int64(3)).Return(tc.events, nil)
			}

			if tc.wantCall {
				stats.EXPECT().DistinctTags(gomock.Any(), int64(3), unboundedSince).Return([]string{}, nil)
			}

			got, err := svc.Tags(ctx, 3)

			if tc.wantMsg != "" {
				assert.Nil(t, got)
				assertLimitReached(t, err, tc.wantMsg)

				return
			}

			require.NoError(t, err)
		})
	}
}

// The gate covers every stats entry point — all four answer 403 LIMIT_REACHED
// while the org is over its viewable bound.
func TestStatsService_ViewableEventsGate_AllEndpoints(t *testing.T) {
	policy := windowPolicy{window: limits.Window{ViewableEvents: 10}}

	calls := []struct {
		desc string
		call func(svc *StatsService, ctx *gofr.Context) error
	}{
		{"link report", func(svc *StatsService, ctx *gofr.Context) error {
			_, err := svc.LinkReport(ctx, 3, 7, 9, statsFrom, statsTo, false)

			return err
		}},
		{"unique clicks", func(svc *StatsService, ctx *gofr.Context) error {
			_, err := svc.UniqueClicks(ctx, 3, []int64{1})

			return err
		}},
		{"tags", func(svc *StatsService, ctx *gofr.Context) error {
			_, err := svc.Tags(ctx, 3)

			return err
		}},
		{"utm analysis", func(svc *StatsService, ctx *gofr.Context) error {
			_, err := svc.UTMAnalysis(ctx, 3, 7)

			return err
		}},
	}

	for _, tc := range calls {
		t.Run(tc.desc, func(t *testing.T) {
			svc, _, _, _, usageReader, ctx := newStatsServiceWithPolicy(t, policy)
			usageReader.EXPECT().EventsThisMonth(gomock.Any(), int64(3)).Return(int64(11), nil)

			err := tc.call(svc, ctx)

			assertLimitReached(t, err, limits.DefaultWindowMessage)
		})
	}
}

// Policy or usage failures behind the window are internal errors (500-path),
// never denials — and never silently unbounded access.
func TestStatsService_WindowInternalErrors(t *testing.T) {
	boom := errors.New("window backend unreachable")

	t.Run("policy error", func(t *testing.T) {
		svc, _, _, _, _, ctx := newStatsServiceWithPolicy(t, windowPolicy{err: boom})

		_, err := svc.Tags(ctx, 3)

		require.ErrorIs(t, err, boom)
	})

	t.Run("usage error", func(t *testing.T) {
		svc, _, _, _, usageReader, ctx := newStatsServiceWithPolicy(t,
			windowPolicy{window: limits.Window{ViewableEvents: 10}})
		usageReader.EXPECT().EventsThisMonth(gomock.Any(), int64(3)).Return(int64(0), boom)

		_, err := svc.Tags(ctx, 3)

		require.ErrorIs(t, err, boom)
	})
}

// Retention clamp math: today minus the retention, in stats date form.
func TestRetentionSinceAndClampFrom(t *testing.T) {
	fixStatsNow(t, "2026-07-21T10:30:00Z")

	tests := []struct {
		desc      string
		retention time.Duration
		from      string
		wantSince string
		wantFrom  string
	}{
		{
			desc:      "no retention: unbounded since, from untouched",
			retention: 0,
			from:      "2020-01-01",
			wantSince: unboundedSince,
			wantFrom:  "2020-01-01",
		},
		{
			desc:      "30-day retention clamps an older from",
			retention: 30 * 24 * time.Hour,
			from:      "2026-05-01",
			wantSince: "2026-06-21",
			wantFrom:  "2026-06-21",
		},
		{
			desc:      "from inside the window is untouched",
			retention: 30 * 24 * time.Hour,
			from:      "2026-07-01",
			wantSince: "2026-06-21",
			wantFrom:  "2026-07-01",
		},
		{
			desc:      "from exactly at the cutoff is untouched",
			retention: 30 * 24 * time.Hour,
			from:      "2026-06-21",
			wantSince: "2026-06-21",
			wantFrom:  "2026-06-21",
		},
		{
			desc:      "retention crossing a month boundary",
			retention: 21 * 24 * time.Hour,
			from:      "2026-06-25",
			wantSince: "2026-06-30",
			wantFrom:  "2026-06-30",
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			window := limits.Window{Retention: tc.retention}

			assert.Equal(t, tc.wantSince, retentionSince(window))
			assert.Equal(t, tc.wantFrom, clampFrom(tc.from, window))
		})
	}
}

// The clamped from-date reaches every one of the report's store queries.
func TestStatsService_LinkReport_RetentionClampsFrom(t *testing.T) {
	fixStatsNow(t, "2026-07-21T10:30:00Z")

	// 30-day retention → cutoff 2026-06-21; the request asks from 2026-06-01.
	policy := windowPolicy{window: limits.Window{Retention: 30 * 24 * time.Hour}}
	svc, stats, links, rules, _, ctx := newStatsServiceWithPolicy(t, policy)

	const clampedFrom = "2026-06-21"

	links.EXPECT().GetByID(gomock.Any(), int64(3), int64(9)).
		Return(publicLink(9, "abc1234", "https://x.co"), nil)
	expectCoreStatsRange(stats, clampedFrom, statsTo)
	stats.EXPECT().DeeplinkClickCount(gomock.Any(), int64(3), int64(9), clampedFrom, statsTo, false).Return(int64(0), nil)
	stats.EXPECT().DeeplinkClickCount(gomock.Any(), int64(3), int64(9), clampedFrom, statsTo, true).Return(int64(0), nil)
	rules.EXPECT().ListByLink(gomock.Any(), int64(3), int64(9)).Return([]models.Rule{}, nil)
	stats.EXPECT().ClickDetails(gomock.Any(), int64(3), int64(9), clampedFrom, statsTo).
		Return([]models.ClickDetail{}, nil)

	report, err := svc.LinkReport(ctx, 3, 7, 9, statsFrom, statsTo, false)

	require.NoError(t, err)
	assert.Equal(t, clampedFrom, report.From, "the report is honest about the clamped range")
	assert.Equal(t, statsTo, report.To)
}

// A requested range entirely before the cutoff clamps to from > to: the store
// answers zero rows and the caller gets an empty report, not an error.
func TestStatsService_LinkReport_RangeFullyOutsideRetention(t *testing.T) {
	fixStatsNow(t, "2026-07-21T10:30:00Z")

	policy := windowPolicy{window: limits.Window{Retention: 7 * 24 * time.Hour}} // cutoff 2026-07-14
	svc, stats, links, rules, _, ctx := newStatsServiceWithPolicy(t, policy)

	const clampedFrom = "2026-07-14"

	links.EXPECT().GetByID(gomock.Any(), int64(3), int64(9)).
		Return(publicLink(9, "abc1234", "https://x.co"), nil)
	expectCoreStatsEmpty(stats, clampedFrom, statsTo)
	stats.EXPECT().DeeplinkClickCount(gomock.Any(), int64(3), int64(9), clampedFrom, statsTo, false).Return(int64(0), nil)
	stats.EXPECT().DeeplinkClickCount(gomock.Any(), int64(3), int64(9), clampedFrom, statsTo, true).Return(int64(0), nil)
	rules.EXPECT().ListByLink(gomock.Any(), int64(3), int64(9)).Return([]models.Rule{}, nil)
	stats.EXPECT().ClickDetails(gomock.Any(), int64(3), int64(9), clampedFrom, statsTo).
		Return([]models.ClickDetail{}, nil)

	report, err := svc.LinkReport(ctx, 3, 7, 9, statsFrom, statsTo, false)

	require.NoError(t, err)
	assert.Zero(t, report.TotalClicks)
}

// expectCoreStatsEmpty wires the sixteen core queries to empty results for
// the out-of-retention range (from > to yields zero rows in the store).
func expectCoreStatsEmpty(stats *MockStatsStore, from, to string) {
	stats.EXPECT().TotalClicks(gomock.Any(), int64(3), int64(9), from, to).Return(int64(0), nil)
	stats.EXPECT().ClicksPerDay(gomock.Any(), int64(3), int64(9), from, to).Return([]models.DayCount{}, nil)

	stats.EXPECT().BrowserPerDay(gomock.Any(), int64(3), int64(9), from, to).Return([]models.DayDimCount{}, nil)
	stats.EXPECT().DevicePerDay(gomock.Any(), int64(3), int64(9), from, to).Return([]models.DayDimCount{}, nil)
	stats.EXPECT().ReferrerPerDay(gomock.Any(), int64(3), int64(9), from, to).Return([]models.DayDimCount{}, nil)
	stats.EXPECT().MobileOSPerDay(gomock.Any(), int64(3), int64(9), from, to).Return([]models.DayDimCount{}, nil)
	stats.EXPECT().CountryPerDay(gomock.Any(), int64(3), int64(9), from, to).Return([]models.DayDimCount{}, nil)
	stats.EXPECT().RegionPerDay(gomock.Any(), int64(3), int64(9), from, to).Return([]models.DayDimCount{}, nil)
	stats.EXPECT().CityPerDay(gomock.Any(), int64(3), int64(9), from, to).Return([]models.DayDimCount{}, nil)

	stats.EXPECT().BrowserTotals(gomock.Any(), int64(3), int64(9), from, to).Return([]models.DimCount{}, nil)
	stats.EXPECT().DeviceTotals(gomock.Any(), int64(3), int64(9), from, to).Return([]models.DimCount{}, nil)
	stats.EXPECT().ReferrerTotals(gomock.Any(), int64(3), int64(9), from, to).Return([]models.DimCount{}, nil)
	stats.EXPECT().MobileOSTotals(gomock.Any(), int64(3), int64(9), from, to).Return([]models.DimCount{}, nil)
	stats.EXPECT().CountryTotals(gomock.Any(), int64(3), int64(9), from, to).Return([]models.DimCount{}, nil)
	stats.EXPECT().RegionTotals(gomock.Any(), int64(3), int64(9), from, to).Return([]models.DimCount{}, nil)
	stats.EXPECT().CityTotals(gomock.Any(), int64(3), int64(9), from, to).Return([]models.DimCount{}, nil)
}

// The retention cutoff reaches the org-level queries as their since bound.
func TestStatsService_OrgQueries_RetentionSince(t *testing.T) {
	fixStatsNow(t, "2026-07-21T10:30:00Z")

	policy := windowPolicy{window: limits.Window{Retention: 30 * 24 * time.Hour}}
	const since = "2026-06-21"

	t.Run("tags", func(t *testing.T) {
		svc, stats, _, _, _, ctx := newStatsServiceWithPolicy(t, policy)
		stats.EXPECT().DistinctTags(gomock.Any(), int64(3), since).Return([]string{"promo"}, nil)

		got, err := svc.Tags(ctx, 3)

		require.NoError(t, err)
		assert.Equal(t, []string{"promo"}, got)
	})

	t.Run("unique clicks", func(t *testing.T) {
		svc, stats, _, _, _, ctx := newStatsServiceWithPolicy(t, policy)
		stats.EXPECT().UniqueTagClicks(gomock.Any(), int64(3), []int64{1, 2}, since).Return(int64(4), nil)

		got, err := svc.UniqueClicks(ctx, 3, []int64{1, 2})

		require.NoError(t, err)
		assert.Equal(t, int64(4), got.UniqueClicks)
	})

	t.Run("utm analysis", func(t *testing.T) {
		svc, stats, _, _, _, ctx := newStatsServiceWithPolicy(t, policy)
		stats.EXPECT().UTMSourceCounts(gomock.Any(), int64(3), int64(7), since).Return([]models.UTMCount{}, nil)
		stats.EXPECT().UTMMediumCounts(gomock.Any(), int64(3), int64(7), since).Return([]models.UTMCount{}, nil)
		stats.EXPECT().UTMCampaignCounts(gomock.Any(), int64(3), int64(7), since).Return([]models.UTMCount{}, nil)

		_, err := svc.UTMAnalysis(ctx, 3, 7)

		require.NoError(t, err)
	})
}

// The gated 403 must carry the machine-readable code so the SPA can render
// the notice — assert the exact envelope contract once more at this layer.
func TestStatsService_GateErrorShape(t *testing.T) {
	svc, _, _, _, usageReader, ctx := newStatsServiceWithPolicy(t,
		windowPolicy{window: limits.Window{ViewableEvents: 1}})
	usageReader.EXPECT().EventsThisMonth(gomock.Any(), int64(3)).Return(int64(2), nil)

	_, err := svc.Tags(ctx, 3)

	var apiErr apierrors.Error
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, map[string]any{"code": apierrors.CodeLimitReached}, apiErr.Response())
}
