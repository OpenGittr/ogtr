package services

import (
	"time"

	"gofr.dev/pkg/gofr"

	"github.com/opengittr/ogtr/backend/apierrors"
	"github.com/opengittr/ogtr/backend/limits"
	"github.com/opengittr/ogtr/backend/models"
)

const statsDateLayout = "2006-01-02"

// unboundedSince is the retention bound handed to the org-level store queries
// when the policy's window has no retention: everything ever recorded is
// after it, so one query shape serves both cases.
const unboundedSince = "1970-01-01"

// maxUniqueClickIDs caps the link_ids list of /api/v1/stats/unique-clicks —
// enough for any dashboard selection while bounding the IN clause.
const maxUniqueClickIDs = 100

// statsNow supplies the clock for the analytics window math (retention
// cutoff); a package variable so the clamping is unit-testable.
var statsNow = time.Now

// StatsService assembles the analytics reports (FEATURES.md §5, §6.3). Link
// visibility mirrors link detail: org-scoped, PRIVATE links creator-only.
//
// Every stats entry point first consults the deployment's limits.Policy for
// the org's analytics Window (ARCHITECTURE.md §8): the retention bound clamps
// how far back queries look, and the viewable-events bound gates viewing
// entirely (403 LIMIT_REACHED) once the org's current-month event count
// exceeds it. Both bound VIEWING only — clicks keep recording and redirects
// keep working regardless (FEATURES.md INV-7: the resolve path has no policy
// dependency at all).
type StatsService struct {
	stats  StatsStore
	links  LinkStore
	rules  RuleStore
	policy limits.Policy
	usage  UsageReader
}

// NewStatsService wires a StatsService. policy supplies the analytics window
// (wire limits.Unlimited{} unless the deployment supplies its own); usage
// supplies the current-month event count the viewable-events gate compares
// against.
func NewStatsService(stats StatsStore, links LinkStore, rules RuleStore,
	policy limits.Policy, usageReader UsageReader) *StatsService {
	return &StatsService{stats: stats, links: links, rules: rules, policy: policy, usage: usageReader}
}

// analyticsWindow fetches the org's Window and applies the viewable-events
// gate: when the bound is set and the org's current-calendar-month event
// count exceeds it, every stats endpoint answers 403 LIMIT_REACHED with the
// policy's message (limits.DefaultWindowMessage when the policy set none). A
// policy or usage error is an internal failure, never a denial.
func (s *StatsService) analyticsWindow(ctx *gofr.Context, orgID int64) (limits.Window, error) {
	window, err := s.policy.AnalyticsWindow(ctx, orgID)
	if err != nil {
		return limits.Window{}, err
	}

	if window.ViewableEvents <= 0 {
		return window, nil
	}

	events, err := s.usage.EventsThisMonth(ctx, orgID)
	if err != nil {
		return limits.Window{}, err
	}

	if events > window.ViewableEvents {
		msg := window.Message
		if msg == "" {
			msg = limits.DefaultWindowMessage
		}

		return limits.Window{}, apierrors.LimitReached(msg)
	}

	return window, nil
}

// retentionSince converts a Window's retention into the since date the
// org-level store queries bind: today minus the retention, or unboundedSince
// when the window has none.
func retentionSince(window limits.Window) string {
	if window.Retention <= 0 {
		return unboundedSince
	}

	return statsNow().Add(-window.Retention).Format(statsDateLayout)
}

// clampFrom clamps a report's from-date to the window's retention cutoff:
// with retention R, nothing older than now-R is viewable, whatever range was
// requested. A range that lies entirely before the cutoff ends up with
// from > to, which the store queries answer with zero rows — an empty report,
// not an error.
func clampFrom(from string, window limits.Window) string {
	if window.Retention <= 0 {
		return from
	}

	if cutoff := retentionSince(window); from < cutoff {
		return cutoff
	}

	return from
}

// LinkReport builds the full per-link report (FEATURES.md §5.1) for [from, to]
// (YYYY-MM-DD, both inclusive; empty = last month). from > to and malformed
// dates are 400. deeplink filters DeeplinkStats.DeeplinkClicks.
func (s *StatsService) LinkReport(ctx *gofr.Context, orgID, viewerID, linkID int64,
	fromStr, toStr string, deeplink bool) (*models.LinkStatsReport, error) {
	from, to, err := resolveDateRange(fromStr, toStr)
	if err != nil {
		return nil, err
	}

	// Analytics window: gate viewing first (uniformly, before any lookup),
	// then clamp the range to the retention bound.
	window, err := s.analyticsWindow(ctx, orgID)
	if err != nil {
		return nil, err
	}

	from = clampFrom(from, window)

	link, err := s.links.GetByID(ctx, orgID, linkID)
	if err != nil {
		return nil, err
	}

	if link == nil || !visibleTo(link, viewerID) {
		return nil, apierrors.NotFound("link not found")
	}

	report := &models.LinkStatsReport{From: from, To: to}

	if err := s.fillCoreStats(ctx, report, orgID, linkID, from, to); err != nil {
		return nil, err
	}

	if err := s.fillDeeplinkStats(ctx, report, orgID, linkID, from, to, deeplink); err != nil {
		return nil, err
	}

	if err := s.fillTargetRuleStats(ctx, report, orgID, linkID, from, to); err != nil {
		return nil, err
	}

	report.Clicks, err = s.stats.ClickDetails(ctx, orgID, linkID, from, to)
	if err != nil {
		return nil, err
	}

	return report, nil
}

// fillCoreStats loads the total, the time series and the dimension breakdowns
// (browser / device / referrer / mobile OS plus the three location levels),
// each as per-day series and whole-range totals.
func (s *StatsService) fillCoreStats(ctx *gofr.Context, r *models.LinkStatsReport,
	orgID, linkID int64, from, to string) error {
	var err error

	if r.TotalClicks, err = s.stats.TotalClicks(ctx, orgID, linkID, from, to); err != nil {
		return err
	}

	if r.ClicksPerDay, err = s.stats.ClicksPerDay(ctx, orgID, linkID, from, to); err != nil {
		return err
	}

	perDay := &r.PerDayBreakdowns

	for _, load := range []struct {
		dst   *[]models.DayDimCount
		fetch func(*gofr.Context, int64, int64, string, string) ([]models.DayDimCount, error)
	}{
		{&perDay.Browser, s.stats.BrowserPerDay},
		{&perDay.DeviceType, s.stats.DevicePerDay},
		{&perDay.Referrer, s.stats.ReferrerPerDay},
		{&perDay.MobileOS, s.stats.MobileOSPerDay},
		{&perDay.Location.Countries, s.stats.CountryPerDay},
		{&perDay.Location.Regions, s.stats.RegionPerDay},
		{&perDay.Location.Cities, s.stats.CityPerDay},
	} {
		if *load.dst, err = load.fetch(ctx, orgID, linkID, from, to); err != nil {
			return err
		}
	}

	totals := &r.TotalBreakdowns

	for _, load := range []struct {
		dst   *[]models.DimCount
		fetch func(*gofr.Context, int64, int64, string, string) ([]models.DimCount, error)
	}{
		{&totals.Browser, s.stats.BrowserTotals},
		{&totals.DeviceType, s.stats.DeviceTotals},
		{&totals.Referrer, s.stats.ReferrerTotals},
		{&totals.MobileOS, s.stats.MobileOSTotals},
		{&totals.Location.Countries, s.stats.CountryTotals},
		{&totals.Location.Regions, s.stats.RegionTotals},
		{&totals.Location.Cities, s.stats.CityTotals},
	} {
		if *load.dst, err = load.fetch(ctx, orgID, linkID, from, to); err != nil {
			return err
		}
	}

	return nil
}

// fillDeeplinkStats loads the two deep-link counters: DeeplinkClicks honors
// the request's ?deeplink= flag (FEATURES.md §5.1), MobileAppOpens always counts actual
// app-intent serves (is_deeplink = TRUE).
func (s *StatsService) fillDeeplinkStats(ctx *gofr.Context, r *models.LinkStatsReport,
	orgID, linkID int64, from, to string, deeplink bool) error {
	flagged, err := s.stats.DeeplinkClickCount(ctx, orgID, linkID, from, to, deeplink)
	if err != nil {
		return err
	}

	opens := flagged
	if !deeplink {
		if opens, err = s.stats.DeeplinkClickCount(ctx, orgID, linkID, from, to, true); err != nil {
			return err
		}
	}

	r.Deeplink = models.DeeplinkStats{DeeplinkClicks: flagged, MobileAppOpens: opens}

	return nil
}

// fillTargetRuleStats sets the target-rule effectiveness section — only when
// the link has rules, otherwise it stays null (FEATURES.md §5.1).
func (s *StatsService) fillTargetRuleStats(ctx *gofr.Context, r *models.LinkStatsReport,
	orgID, linkID int64, from, to string) error {
	rules, err := s.rules.ListByLink(ctx, orgID, linkID)
	if err != nil {
		return err
	}

	if len(rules) == 0 {
		return nil
	}

	matched, err := s.stats.TargetMatchedCount(ctx, orgID, linkID, from, to)
	if err != nil {
		return err
	}

	r.TargetRule = &models.TargetRuleStats{TotalClicks: r.TotalClicks, TargetMatched: matched}

	return nil
}

// UniqueClicks counts distinct campaign tags over the given links' clicks,
// org-scoped (§5.2). An empty id list is 400.
func (s *StatsService) UniqueClicks(ctx *gofr.Context, orgID int64, linkIDs []int64) (*models.UniqueClicksResult, error) {
	if len(linkIDs) == 0 {
		return nil, apierrors.BadRequest("link_ids must not be empty")
	}

	if len(linkIDs) > maxUniqueClickIDs {
		return nil, apierrors.BadRequest("link_ids supports at most 100 ids")
	}

	window, err := s.analyticsWindow(ctx, orgID)
	if err != nil {
		return nil, err
	}

	n, err := s.stats.UniqueTagClicks(ctx, orgID, linkIDs, retentionSince(window))
	if err != nil {
		return nil, err
	}

	return &models.UniqueClicksResult{UniqueClicks: n}, nil
}

// Tags lists every distinct campaign tag in the org's click data (§5.3),
// within the analytics window.
func (s *StatsService) Tags(ctx *gofr.Context, orgID int64) ([]string, error) {
	window, err := s.analyticsWindow(ctx, orgID)
	if err != nil {
		return nil, err
	}

	return s.stats.DistinctTags(ctx, orgID, retentionSince(window))
}

// UTMAnalysis returns the three UTM analyses (§6.3) over the viewer-visible
// links of the org: click counts per UTM value per link, within the analytics
// window.
func (s *StatsService) UTMAnalysis(ctx *gofr.Context, orgID, viewerID int64) (*models.UTMAnalysis, error) {
	window, err := s.analyticsWindow(ctx, orgID)
	if err != nil {
		return nil, err
	}

	since := retentionSince(window)

	source, err := s.stats.UTMSourceCounts(ctx, orgID, viewerID, since)
	if err != nil {
		return nil, err
	}

	medium, err := s.stats.UTMMediumCounts(ctx, orgID, viewerID, since)
	if err != nil {
		return nil, err
	}

	campaign, err := s.stats.UTMCampaignCounts(ctx, orgID, viewerID, since)
	if err != nil {
		return nil, err
	}

	return &models.UTMAnalysis{
		SourceAnalysis:   source,
		MediumAnalysis:   medium,
		CampaignAnalysis: campaign,
	}, nil
}

// resolveDateRange validates the YYYY-MM-DD pair and applies the default
// window (last month .. today). Malformed dates and from > to are 400.
func resolveDateRange(fromStr, toStr string) (from, to string, err error) {
	now := time.Now()

	fromT := now.AddDate(0, -1, 0)
	if fromStr != "" {
		if fromT, err = time.Parse(statsDateLayout, fromStr); err != nil {
			return "", "", apierrors.BadRequest("from must be a YYYY-MM-DD date")
		}
	}

	toT := now
	if toStr != "" {
		if toT, err = time.Parse(statsDateLayout, toStr); err != nil {
			return "", "", apierrors.BadRequest("to must be a YYYY-MM-DD date")
		}
	}

	from, to = fromT.Format(statsDateLayout), toT.Format(statsDateLayout)

	if from > to {
		return "", "", apierrors.BadRequest("from must not be after to")
	}

	return from, to, nil
}
