package stores

import (
	"strings"

	"gofr.dev/pkg/gofr"

	"github.com/opengittr/ogtr/backend/models"
)

// StatsStore reads aggregate click analytics (FEATURES.md §5, §6.3). Every
// query is org-scoped and parameterized; the date range is inclusive of both
// end days: ts >= from 00:00 AND ts < to + 1 day.
type StatsStore struct{}

// NewStatsStore builds a StatsStore.
func NewStatsStore() *StatsStore { return &StatsStore{} }

// rangeFilter is the shared org + link + date-range WHERE clause. from/to bind
// as YYYY-MM-DD strings.
const rangeFilter = "org_id = ? AND link_id = ? AND ts >= ? AND ts < DATE_ADD(?, INTERVAL 1 DAY)"

const (
	totalClicksQuery = "SELECT COUNT(id) FROM clicks WHERE " + rangeFilter

	clicksPerDayQuery = "SELECT DATE_FORMAT(ts, '%Y-%m-%d'), COUNT(id) FROM clicks WHERE " +
		rangeFilter + " GROUP BY DATE_FORMAT(ts, '%Y-%m-%d') ORDER BY DATE_FORMAT(ts, '%Y-%m-%d')"

	// Per-day dimension breakdowns. NULL dimension values group as ''.
	browserPerDayQuery = "SELECT DATE_FORMAT(ts, '%Y-%m-%d'), COALESCE(browser, ''), COUNT(id) FROM clicks WHERE " +
		rangeFilter + " GROUP BY DATE_FORMAT(ts, '%Y-%m-%d'), browser ORDER BY DATE_FORMAT(ts, '%Y-%m-%d'), browser"

	devicePerDayQuery = "SELECT DATE_FORMAT(ts, '%Y-%m-%d'), COALESCE(device_type, ''), COUNT(id) FROM clicks WHERE " +
		rangeFilter + " GROUP BY DATE_FORMAT(ts, '%Y-%m-%d'), device_type ORDER BY DATE_FORMAT(ts, '%Y-%m-%d'), device_type"

	referrerPerDayQuery = "SELECT DATE_FORMAT(ts, '%Y-%m-%d'), COALESCE(referrer, ''), COUNT(id) FROM clicks WHERE " +
		rangeFilter + " GROUP BY DATE_FORMAT(ts, '%Y-%m-%d'), referrer ORDER BY DATE_FORMAT(ts, '%Y-%m-%d'), referrer"

	// mobile_os excludes 'NA' (desktop) rows (FEATURES.md §5.1).
	mobileOSPerDayQuery = "SELECT DATE_FORMAT(ts, '%Y-%m-%d'), COALESCE(mobile_os, ''), COUNT(id) FROM clicks WHERE " +
		rangeFilter + " AND mobile_os IS NOT NULL AND mobile_os != 'NA'" +
		" GROUP BY DATE_FORMAT(ts, '%Y-%m-%d'), mobile_os ORDER BY DATE_FORMAT(ts, '%Y-%m-%d'), mobile_os"

	// Location breakdowns (country / region / city). NULL geo values (GeoIP
	// off, unknown IP) roll up as 'Unknown'; ties order by the label, so the
	// ordering matches the COALESCEd value the API serves.
	countryPerDayQuery = "SELECT DATE_FORMAT(ts, '%Y-%m-%d'), COALESCE(country, 'Unknown'), COUNT(id) FROM clicks WHERE " +
		rangeFilter + " GROUP BY DATE_FORMAT(ts, '%Y-%m-%d'), country" +
		" ORDER BY DATE_FORMAT(ts, '%Y-%m-%d'), COALESCE(country, 'Unknown')"

	regionPerDayQuery = "SELECT DATE_FORMAT(ts, '%Y-%m-%d'), COALESCE(region, 'Unknown'), COUNT(id) FROM clicks WHERE " +
		rangeFilter + " GROUP BY DATE_FORMAT(ts, '%Y-%m-%d'), region" +
		" ORDER BY DATE_FORMAT(ts, '%Y-%m-%d'), COALESCE(region, 'Unknown')"

	cityPerDayQuery = "SELECT DATE_FORMAT(ts, '%Y-%m-%d'), COALESCE(city, 'Unknown'), COUNT(id) FROM clicks WHERE " +
		rangeFilter + " GROUP BY DATE_FORMAT(ts, '%Y-%m-%d'), city" +
		" ORDER BY DATE_FORMAT(ts, '%Y-%m-%d'), COALESCE(city, 'Unknown')"

	countryTotalsQuery = "SELECT COALESCE(country, 'Unknown'), COUNT(id) FROM clicks WHERE " +
		rangeFilter + " GROUP BY country ORDER BY COUNT(id) DESC, COALESCE(country, 'Unknown')"

	regionTotalsQuery = "SELECT COALESCE(region, 'Unknown'), COUNT(id) FROM clicks WHERE " +
		rangeFilter + " GROUP BY region ORDER BY COUNT(id) DESC, COALESCE(region, 'Unknown')"

	cityTotalsQuery = "SELECT COALESCE(city, 'Unknown'), COUNT(id) FROM clicks WHERE " +
		rangeFilter + " GROUP BY city ORDER BY COUNT(id) DESC, COALESCE(city, 'Unknown')"

	// Whole-range dimension totals.
	browserTotalsQuery = "SELECT COALESCE(browser, ''), COUNT(id) FROM clicks WHERE " +
		rangeFilter + " GROUP BY browser ORDER BY COUNT(id) DESC, browser"

	deviceTotalsQuery = "SELECT COALESCE(device_type, ''), COUNT(id) FROM clicks WHERE " +
		rangeFilter + " GROUP BY device_type ORDER BY COUNT(id) DESC, device_type"

	referrerTotalsQuery = "SELECT COALESCE(referrer, ''), COUNT(id) FROM clicks WHERE " +
		rangeFilter + " GROUP BY referrer ORDER BY COUNT(id) DESC, referrer"

	mobileOSTotalsQuery = "SELECT COALESCE(mobile_os, ''), COUNT(id) FROM clicks WHERE " +
		rangeFilter + " AND mobile_os IS NOT NULL AND mobile_os != 'NA'" +
		" GROUP BY mobile_os ORDER BY COUNT(id) DESC, mobile_os"

	deeplinkCountQuery = "SELECT COUNT(id) FROM clicks WHERE " + rangeFilter + " AND is_deeplink = ?"

	targetMatchedQuery = "SELECT COUNT(id) FROM clicks WHERE " + rangeFilter + " AND target_matched = TRUE"

	clickDetailsQuery = "SELECT id, link_id, custom_tag_id, ts FROM clicks WHERE " +
		rangeFilter + " ORDER BY ts DESC, id DESC"

	// The org-level queries below carry a `ts >= ?` retention bound (`since`,
	// YYYY-MM-DD): the stats service binds the policy's retention cutoff, or
	// 1970-01-01 when unbounded — one query shape either way.
	distinctTagsQuery = "SELECT DISTINCT custom_tag_id FROM clicks" +
		" WHERE org_id = ? AND ts >= ? AND custom_tag_id IS NOT NULL ORDER BY custom_tag_id"

	// visibleLinkJoinFilter scopes UTM analyses to links the viewer can see —
	// org-scoped, other users' PRIVATE links excluded (FEATURES.md §1.1).
	visibleLinkJoinFilter = "c.org_id = ? AND (l.type = 'PUBLIC' OR l.user_id = ?) AND c.ts >= ?"

	utmSourceQuery = "SELECT c.utm_source, l.id, l.code, l.destination_url, COUNT(c.id)" +
		" FROM clicks c INNER JOIN links l ON l.id = c.link_id WHERE " + visibleLinkJoinFilter +
		" AND c.utm_source IS NOT NULL AND c.utm_source != ''" +
		" GROUP BY c.utm_source, l.id, l.code, l.destination_url" +
		" ORDER BY COUNT(c.id) DESC, c.utm_source, l.id"

	utmMediumQuery = "SELECT c.utm_medium, l.id, l.code, l.destination_url, COUNT(c.id)" +
		" FROM clicks c INNER JOIN links l ON l.id = c.link_id WHERE " + visibleLinkJoinFilter +
		" AND c.utm_medium IS NOT NULL AND c.utm_medium != ''" +
		" GROUP BY c.utm_medium, l.id, l.code, l.destination_url" +
		" ORDER BY COUNT(c.id) DESC, c.utm_medium, l.id"

	utmCampaignQuery = "SELECT c.utm_campaign, l.id, l.code, l.destination_url, COUNT(c.id)" +
		" FROM clicks c INNER JOIN links l ON l.id = c.link_id WHERE " + visibleLinkJoinFilter +
		" AND c.utm_campaign IS NOT NULL AND c.utm_campaign != ''" +
		" GROUP BY c.utm_campaign, l.id, l.code, l.destination_url" +
		" ORDER BY COUNT(c.id) DESC, c.utm_campaign, l.id"
)

// TotalClicks counts the link's clicks in range.
func (*StatsStore) TotalClicks(ctx *gofr.Context, orgID, linkID int64, from, to string) (int64, error) {
	return countQuery(ctx, totalClicksQuery, orgID, linkID, from, to)
}

// ClicksPerDay returns the clicks-per-day time series (days with clicks only).
func (*StatsStore) ClicksPerDay(ctx *gofr.Context, orgID, linkID int64, from, to string) ([]models.DayCount, error) {
	rows, err := ctx.SQL.QueryContext(ctx, clicksPerDayQuery, orgID, linkID, from, to)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	series := []models.DayCount{}

	for rows.Next() {
		var d models.DayCount

		if err := rows.Scan(&d.Date, &d.Clicks); err != nil {
			return nil, err
		}

		series = append(series, d)
	}

	return series, rows.Err()
}

// BrowserPerDay returns the per-day browser breakdown.
func (*StatsStore) BrowserPerDay(ctx *gofr.Context, orgID, linkID int64, from, to string) ([]models.DayDimCount, error) {
	return dayDimQuery(ctx, browserPerDayQuery, orgID, linkID, from, to)
}

// DevicePerDay returns the per-day device-type breakdown.
func (*StatsStore) DevicePerDay(ctx *gofr.Context, orgID, linkID int64, from, to string) ([]models.DayDimCount, error) {
	return dayDimQuery(ctx, devicePerDayQuery, orgID, linkID, from, to)
}

// ReferrerPerDay returns the per-day referrer breakdown.
func (*StatsStore) ReferrerPerDay(ctx *gofr.Context, orgID, linkID int64, from, to string) ([]models.DayDimCount, error) {
	return dayDimQuery(ctx, referrerPerDayQuery, orgID, linkID, from, to)
}

// MobileOSPerDay returns the per-day mobile-OS breakdown ('NA' excluded).
func (*StatsStore) MobileOSPerDay(ctx *gofr.Context, orgID, linkID int64, from, to string) ([]models.DayDimCount, error) {
	return dayDimQuery(ctx, mobileOSPerDayQuery, orgID, linkID, from, to)
}

// CountryPerDay returns the per-day country breakdown ('Unknown' for no geo).
func (*StatsStore) CountryPerDay(ctx *gofr.Context, orgID, linkID int64, from, to string) ([]models.DayDimCount, error) {
	return dayDimQuery(ctx, countryPerDayQuery, orgID, linkID, from, to)
}

// RegionPerDay returns the per-day region breakdown ('Unknown' for no geo).
func (*StatsStore) RegionPerDay(ctx *gofr.Context, orgID, linkID int64, from, to string) ([]models.DayDimCount, error) {
	return dayDimQuery(ctx, regionPerDayQuery, orgID, linkID, from, to)
}

// CityPerDay returns the per-day city breakdown ('Unknown' for no geo).
func (*StatsStore) CityPerDay(ctx *gofr.Context, orgID, linkID int64, from, to string) ([]models.DayDimCount, error) {
	return dayDimQuery(ctx, cityPerDayQuery, orgID, linkID, from, to)
}

// BrowserTotals returns the whole-range browser totals.
func (*StatsStore) BrowserTotals(ctx *gofr.Context, orgID, linkID int64, from, to string) ([]models.DimCount, error) {
	return dimQuery(ctx, browserTotalsQuery, orgID, linkID, from, to)
}

// DeviceTotals returns the whole-range device-type totals.
func (*StatsStore) DeviceTotals(ctx *gofr.Context, orgID, linkID int64, from, to string) ([]models.DimCount, error) {
	return dimQuery(ctx, deviceTotalsQuery, orgID, linkID, from, to)
}

// ReferrerTotals returns the whole-range referrer totals.
func (*StatsStore) ReferrerTotals(ctx *gofr.Context, orgID, linkID int64, from, to string) ([]models.DimCount, error) {
	return dimQuery(ctx, referrerTotalsQuery, orgID, linkID, from, to)
}

// MobileOSTotals returns the whole-range mobile-OS totals ('NA' excluded).
func (*StatsStore) MobileOSTotals(ctx *gofr.Context, orgID, linkID int64, from, to string) ([]models.DimCount, error) {
	return dimQuery(ctx, mobileOSTotalsQuery, orgID, linkID, from, to)
}

// CountryTotals returns the whole-range country totals ('Unknown' for no geo).
func (*StatsStore) CountryTotals(ctx *gofr.Context, orgID, linkID int64, from, to string) ([]models.DimCount, error) {
	return dimQuery(ctx, countryTotalsQuery, orgID, linkID, from, to)
}

// RegionTotals returns the whole-range region totals ('Unknown' for no geo).
func (*StatsStore) RegionTotals(ctx *gofr.Context, orgID, linkID int64, from, to string) ([]models.DimCount, error) {
	return dimQuery(ctx, regionTotalsQuery, orgID, linkID, from, to)
}

// CityTotals returns the whole-range city totals ('Unknown' for no geo).
func (*StatsStore) CityTotals(ctx *gofr.Context, orgID, linkID int64, from, to string) ([]models.DimCount, error) {
	return dimQuery(ctx, cityTotalsQuery, orgID, linkID, from, to)
}

// DeeplinkClickCount counts clicks in range with the given is_deeplink flag.
func (*StatsStore) DeeplinkClickCount(ctx *gofr.Context, orgID, linkID int64, from, to string, isDeeplink bool) (int64, error) {
	return countQuery(ctx, deeplinkCountQuery, orgID, linkID, from, to, isDeeplink)
}

// TargetMatchedCount counts clicks in range where a target rule matched.
func (*StatsStore) TargetMatchedCount(ctx *gofr.Context, orgID, linkID int64, from, to string) (int64, error) {
	return countQuery(ctx, targetMatchedQuery, orgID, linkID, from, to)
}

// ClickDetails returns the individual click records in range, newest first.
func (*StatsStore) ClickDetails(ctx *gofr.Context, orgID, linkID int64, from, to string) ([]models.ClickDetail, error) {
	rows, err := ctx.SQL.QueryContext(ctx, clickDetailsQuery, orgID, linkID, from, to)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	details := []models.ClickDetail{}

	for rows.Next() {
		var d models.ClickDetail

		if err := rows.Scan(&d.ID, &d.LinkID, &d.CustomTagID, &d.Ts); err != nil {
			return nil, err
		}

		details = append(details, d)
	}

	return details, rows.Err()
}

// UniqueTagClicks counts distinct non-null custom_tag_id values over the given
// links' clicks, org-scoped (ids outside the org simply never match — the
// org_id predicate scopes the count; FEATURES.md §5.2). since is the
// retention bound (1970-01-01 = unbounded). The IN list is built from
// placeholders only — every id binds as a parameter.
func (*StatsStore) UniqueTagClicks(ctx *gofr.Context, orgID int64, linkIDs []int64, since string) (int64, error) {
	placeholders := strings.TrimSuffix(strings.Repeat("?, ", len(linkIDs)), ", ")

	args := make([]any, 0, len(linkIDs)+2)
	args = append(args, orgID)

	for _, id := range linkIDs {
		args = append(args, id)
	}

	args = append(args, since)

	query := "SELECT COUNT(DISTINCT custom_tag_id) FROM clicks WHERE org_id = ? AND link_id IN (" +
		placeholders + ") AND ts >= ?"

	var n int64
	if err := ctx.SQL.QueryRowContext(ctx, query, args...).Scan(&n); err != nil {
		return 0, err
	}

	return n, nil
}

// DistinctTags lists every distinct non-null custom_tag_id in the org's click
// data — the full set in one query (FEATURES.md §5.3). since is the retention
// bound (1970-01-01 = unbounded).
func (*StatsStore) DistinctTags(ctx *gofr.Context, orgID int64, since string) ([]string, error) {
	rows, err := ctx.SQL.QueryContext(ctx, distinctTagsQuery, orgID, since)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	tags := []string{}

	for rows.Next() {
		var tag string

		if err := rows.Scan(&tag); err != nil {
			return nil, err
		}

		tags = append(tags, tag)
	}

	return tags, rows.Err()
}

// UTMSourceCounts groups clicks by utm_source per link (empty values skipped),
// over links the viewer can see in the org. since is the retention bound
// (1970-01-01 = unbounded).
func (*StatsStore) UTMSourceCounts(ctx *gofr.Context, orgID, viewerID int64, since string) ([]models.UTMCount, error) {
	return utmQuery(ctx, utmSourceQuery, orgID, viewerID, since)
}

// UTMMediumCounts groups clicks by utm_medium per link (empty values skipped).
func (*StatsStore) UTMMediumCounts(ctx *gofr.Context, orgID, viewerID int64, since string) ([]models.UTMCount, error) {
	return utmQuery(ctx, utmMediumQuery, orgID, viewerID, since)
}

// UTMCampaignCounts groups clicks by utm_campaign per link (empty values
// skipped).
func (*StatsStore) UTMCampaignCounts(ctx *gofr.Context, orgID, viewerID int64, since string) ([]models.UTMCount, error) {
	return utmQuery(ctx, utmCampaignQuery, orgID, viewerID, since)
}

// countQuery runs a single-COUNT statement.
func countQuery(ctx *gofr.Context, query string, args ...any) (int64, error) {
	var n int64
	if err := ctx.SQL.QueryRowContext(ctx, query, args...).Scan(&n); err != nil {
		return 0, err
	}

	return n, nil
}

// dayDimQuery scans (date, value, count) rows.
func dayDimQuery(ctx *gofr.Context, query string, orgID, linkID int64, from, to string) ([]models.DayDimCount, error) {
	rows, err := ctx.SQL.QueryContext(ctx, query, orgID, linkID, from, to)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := []models.DayDimCount{}

	for rows.Next() {
		var d models.DayDimCount

		if err := rows.Scan(&d.Date, &d.Value, &d.Clicks); err != nil {
			return nil, err
		}

		out = append(out, d)
	}

	return out, rows.Err()
}

// dimQuery scans (value, count) rows.
func dimQuery(ctx *gofr.Context, query string, orgID, linkID int64, from, to string) ([]models.DimCount, error) {
	rows, err := ctx.SQL.QueryContext(ctx, query, orgID, linkID, from, to)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := []models.DimCount{}

	for rows.Next() {
		var d models.DimCount

		if err := rows.Scan(&d.Value, &d.Clicks); err != nil {
			return nil, err
		}

		out = append(out, d)
	}

	return out, rows.Err()
}

// utmQuery scans (utm value, link id, code, url, count) rows.
func utmQuery(ctx *gofr.Context, query string, orgID, viewerID int64, since string) ([]models.UTMCount, error) {
	rows, err := ctx.SQL.QueryContext(ctx, query, orgID, viewerID, since)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := []models.UTMCount{}

	for rows.Next() {
		var u models.UTMCount

		if err := rows.Scan(&u.UTMValue, &u.LinkID, &u.Code, &u.URL, &u.Clicks); err != nil {
			return nil, err
		}

		out = append(out, u)
	}

	return out, rows.Err()
}
