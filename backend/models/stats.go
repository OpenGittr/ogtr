package models

import "time"

// Analytics report types (FEATURES.md §5, §6.3). These are the stable JSON
// shapes served by /api/v1/links/{id}/stats and /api/v1/stats/* — field names
// are part of the API.

// DayCount is one point of the clicks-per-day time series.
type DayCount struct {
	Date   string `json:"date"` // YYYY-MM-DD
	Clicks int64  `json:"clicks"`
}

// DayDimCount is one per-day bucket of a dimension breakdown (browser,
// device type, referrer or mobile OS on a given day).
type DayDimCount struct {
	Date   string `json:"date"` // YYYY-MM-DD
	Value  string `json:"value"`
	Clicks int64  `json:"clicks"`
}

// DimCount is one whole-range bucket of a dimension breakdown.
type DimCount struct {
	Value  string `json:"value"`
	Clicks int64  `json:"clicks"`
}

// LocationPerDay is the per-day location breakdown at its three levels
// (FEATURES.md §5.1 "location breakdown"). Clicks without geo data (GeoIP
// off, unknown IP) bucket as "Unknown".
type LocationPerDay struct {
	Countries []DayDimCount `json:"countries"`
	Regions   []DayDimCount `json:"regions"`
	Cities    []DayDimCount `json:"cities"`
}

// LocationTotals is the whole-range location breakdown at its three levels.
// Clicks without geo data bucket as "Unknown".
type LocationTotals struct {
	Countries []DimCount `json:"countries"`
	Regions   []DimCount `json:"regions"`
	Cities    []DimCount `json:"cities"`
}

// PerDayBreakdowns groups the per-day dimension time series
// (FEATURES.md §5.1 "time-series breakdowns"). mobile_os excludes 'NA'
// (desktop) rows; location covers the three recorded geo levels.
type PerDayBreakdowns struct {
	Browser    []DayDimCount  `json:"browser"`
	DeviceType []DayDimCount  `json:"device_type"`
	Referrer   []DayDimCount  `json:"referrer"`
	MobileOS   []DayDimCount  `json:"mobile_os"`
	Location   LocationPerDay `json:"location"`
}

// TotalBreakdowns groups the whole-range dimension totals
// (FEATURES.md §5.1 "aggregate breakdowns"). mobile_os excludes 'NA';
// location covers the three recorded geo levels.
type TotalBreakdowns struct {
	Browser    []DimCount     `json:"browser"`
	DeviceType []DimCount     `json:"device_type"`
	Referrer   []DimCount     `json:"referrer"`
	MobileOS   []DimCount     `json:"mobile_os"`
	Location   LocationTotals `json:"location"`
}

// DeeplinkStats is the deep-link section of the report: DeeplinkClicks counts
// clicks with is_deeplink equal to the request's ?deeplink= flag (the
// filtered count, FEATURES.md §5.1); MobileAppOpens always counts the
// clicks actually served an app deep link (is_deeplink = TRUE).
type DeeplinkStats struct {
	DeeplinkClicks int64 `json:"deeplink_clicks"`
	MobileAppOpens int64 `json:"mobile_app_opens"`
}

// TargetRuleStats is the target-rule effectiveness section: range total vs.
// clicks where a rule matched. Present only when the link has rules.
type TargetRuleStats struct {
	TotalClicks   int64 `json:"total_clicks"`
	TargetMatched int64 `json:"target_matched"`
}

// ClickDetail is one row of the report's detailed click list.
type ClickDetail struct {
	ID          int64     `json:"id"`
	LinkID      int64     `json:"link_id"`
	CustomTagID *string   `json:"custom_tag_id"`
	Ts          time.Time `json:"ts"`
}

// LinkStatsReport is the full per-link report (FEATURES.md §5.1),
// assembled in one response. TargetRule is null when the link has no rules.
type LinkStatsReport struct {
	From             string           `json:"from"` // resolved range, YYYY-MM-DD
	To               string           `json:"to"`
	TotalClicks      int64            `json:"total_clicks"`
	ClicksPerDay     []DayCount       `json:"clicks_per_day"`
	PerDayBreakdowns PerDayBreakdowns `json:"per_day_breakdowns"`
	TotalBreakdowns  TotalBreakdowns  `json:"total_breakdowns"`
	Deeplink         DeeplinkStats    `json:"deeplink"`
	TargetRule       *TargetRuleStats `json:"target_rule"`
	Clicks           []ClickDetail    `json:"clicks"`
}

// UniqueClicksResult is the /api/v1/stats/unique-clicks response: distinct
// campaign tags that clicked the given links (FEATURES.md §5.2).
type UniqueClicksResult struct {
	UniqueClicks int64 `json:"unique_clicks"`
}

// UTMCount is one row of a UTM analysis: a UTM value on one link
// (FEATURES.md §6.3 "broken down per destination URL").
type UTMCount struct {
	UTMValue string `json:"utm_value"`
	LinkID   int64  `json:"link_id"`
	Code     string `json:"code"`
	URL      string `json:"url"`
	Clicks   int64  `json:"clicks"`
}

// UTMAnalysis is the /api/v1/stats/utm response: three parallel analyses.
type UTMAnalysis struct {
	SourceAnalysis   []UTMCount `json:"source_analysis"`
	MediumAnalysis   []UTMCount `json:"medium_analysis"`
	CampaignAnalysis []UTMCount `json:"campaign_analysis"`
}
