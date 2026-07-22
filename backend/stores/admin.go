package stores

import (
	"database/sql"
	"errors"
	"strings"

	"gofr.dev/pkg/gofr"

	"github.com/opengittr/ogtr/backend/models"
)

// AdminStore serves the instance-admin API's read queries (ARCHITECTURE.md
// "Instance admin API"). Every query here is DELIBERATELY cross-org — the
// one sanctioned exception to INV-6, reachable only through the
// ADMIN_API_TOKEN gate. Keeping all of them in this single file keeps the
// exception auditable.
type AdminStore struct{}

// NewAdminStore builds an AdminStore.
func NewAdminStore() *AdminStore { return &AdminStore{} }

// The search filters match email-or-name (users) and name-or-slug (orgs).
// The raw query is escaped here and bound as an anywhere-matching %pattern%;
// an empty query becomes %%, which matches every (NOT NULL) row — one query
// shape covers filtered and unfiltered listing.
const (
	listUsersQuery = "SELECT id, email, name, created_at, last_active_at FROM users " +
		"WHERE (email LIKE ? OR name LIKE ?) ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?"

	countUsersQuery = "SELECT COUNT(id) FROM users WHERE (email LIKE ? OR name LIKE ?)"

	listOrgsQuery = "SELECT id, name, slug, created_at FROM orgs WHERE (name LIKE ? OR slug LIKE ?) " +
		"ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?"

	countOrgsQuery = "SELECT COUNT(id) FROM orgs WHERE (name LIKE ? OR slug LIKE ?)"

	orgExistsQuery = "SELECT COUNT(id) FROM orgs WHERE id = ?"

	// One org's member list, OWNERs first, then join order. Served by the
	// org_members primary key (org_id, user_id) — a single join, no N+1.
	orgUsersQuery = "SELECT u.id, u.email, u.name, om.role, om.created_at, u.last_active_at " +
		"FROM org_members om INNER JOIN users u ON u.id = om.user_id WHERE om.org_id = ? " +
		"ORDER BY om.role = 'OWNER' DESC, om.created_at, u.id"

	listReportsQuery = "SELECT r.id, r.code, r.link_id, r.org_id, r.reason, r.reporter_contact, r.created_at, " +
		"l.status, l.destination_url FROM abuse_reports r INNER JOIN links l ON l.id = r.link_id " +
		"ORDER BY r.created_at DESC, r.id DESC LIMIT ? OFFSET ?"

	countReportsQuery = "SELECT COUNT(id) FROM abuse_reports"

	adminLinkQuery = "SELECT l.id, l.org_id, l.code, l.destination_url, l.status, l.created_at, l.visits, " +
		"o.name, u.email FROM links l INNER JOIN orgs o ON o.id = l.org_id " +
		"LEFT JOIN users u ON u.id = l.user_id WHERE l.id = ?"

	// Instance-wide per-day series (UTC days computed by the service and
	// bound as YYYY-MM-DD). Grouping is by the selected DATE_FORMAT
	// expression itself (only_full_group_by, same convention as StatsStore).
	signupsPerDayQuery = "SELECT DATE_FORMAT(created_at, '%Y-%m-%d'), COUNT(id) FROM users WHERE created_at >= ? " +
		"GROUP BY DATE_FORMAT(created_at, '%Y-%m-%d') ORDER BY DATE_FORMAT(created_at, '%Y-%m-%d')"

	linksCreatedPerDayQuery = "SELECT DATE_FORMAT(created_at, '%Y-%m-%d'), COUNT(id) FROM links WHERE created_at >= ? " +
		"GROUP BY DATE_FORMAT(created_at, '%Y-%m-%d') ORDER BY DATE_FORMAT(created_at, '%Y-%m-%d')"

	clicksPerDayAdminQuery = "SELECT DATE_FORMAT(ts, '%Y-%m-%d'), COUNT(id) FROM clicks WHERE ts >= ? " +
		"GROUP BY DATE_FORMAT(ts, '%Y-%m-%d') ORDER BY DATE_FORMAT(ts, '%Y-%m-%d')"
)

// Grouped per-org count shapes; the IN list is built from placeholders only.
const (
	orgMemberCounts = "SELECT org_id, COUNT(*) FROM org_members WHERE org_id IN (%s) GROUP BY org_id"
	orgLinkCounts   = "SELECT org_id, COUNT(id) FROM links WHERE org_id IN (%s) GROUP BY org_id"
	orgClickCounts  = "SELECT org_id, COUNT(id) FROM clicks WHERE org_id IN (%s) AND ts >= ? GROUP BY org_id"
	orgDomainCounts = "SELECT org_id, COUNT(id) FROM domains WHERE org_id IN (%s) GROUP BY org_id"

	userOrgsQuery = "SELECT om.user_id, o.id, o.name, om.role FROM org_members om " +
		"INNER JOIN orgs o ON o.id = om.org_id WHERE om.user_id IN (%s) ORDER BY om.user_id, o.id"

	// One owner per org for the orgs listing: each org's first OWNER by join
	// time, picked with a window function over the page's org ids in a
	// single grouped query (no per-org subquery, no N+1).
	orgOwnersQuery = "SELECT org_id, id, email, name FROM (" +
		"SELECT om.org_id, u.id, u.email, u.name, " +
		"ROW_NUMBER() OVER (PARTITION BY om.org_id ORDER BY om.created_at, u.id) AS rn " +
		"FROM org_members om INNER JOIN users u ON u.id = om.user_id " +
		"WHERE om.org_id IN (%s) AND om.role = 'OWNER') ranked WHERE rn = 1"
)

// likePattern escapes MySQL LIKE metacharacters in a user-supplied search
// term (so it matches literally) and wraps it as an anywhere-matching
// pattern.
func likePattern(term string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

	return "%" + r.Replace(term) + "%"
}

// ListUsers returns one page of users whose email or name contains query
// (empty = all), newest first. Org memberships are not filled here — the
// service attaches them via UserOrgs in one grouped query.
func (*AdminStore) ListUsers(ctx *gofr.Context, query string, limit, offset int) ([]models.AdminUser, error) {
	pattern := likePattern(query)

	rows, err := ctx.SQL.QueryContext(ctx, listUsersQuery, pattern, pattern, limit, offset)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	users := []models.AdminUser{}

	for rows.Next() {
		u := models.AdminUser{Orgs: []models.AdminUserOrg{}}

		if err := rows.Scan(&u.ID, &u.Email, &u.Name, &u.CreatedAt, &u.LastActiveAt); err != nil {
			return nil, err
		}

		users = append(users, u)
	}

	return users, rows.Err()
}

// CountUsers counts users whose email or name contains query.
func (*AdminStore) CountUsers(ctx *gofr.Context, query string) (int64, error) {
	pattern := likePattern(query)

	return countQuery(ctx, countUsersQuery, pattern, pattern)
}

// UserOrgs returns every org membership of the given users in one grouped
// query (no per-user N+1), keyed by user id.
func (*AdminStore) UserOrgs(ctx *gofr.Context, userIDs []int64) (map[int64][]models.AdminUserOrg, error) {
	memberships := map[int64][]models.AdminUserOrg{}
	if len(userIDs) == 0 {
		return memberships, nil
	}

	query, args := inQuery(userOrgsQuery, userIDs)

	rows, err := ctx.SQL.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var (
			userID int64
			org    models.AdminUserOrg
		)

		if err := rows.Scan(&userID, &org.ID, &org.Name, &org.Role); err != nil {
			return nil, err
		}

		memberships[userID] = append(memberships[userID], org)
	}

	return memberships, rows.Err()
}

// ListOrgs returns one page of orgs whose name or slug contains query,
// newest first, with zeroed counts — the service fills those via OrgCounts.
func (*AdminStore) ListOrgs(ctx *gofr.Context, query string, limit, offset int) ([]models.AdminOrg, error) {
	pattern := likePattern(query)

	rows, err := ctx.SQL.QueryContext(ctx, listOrgsQuery, pattern, pattern, limit, offset)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	orgs := []models.AdminOrg{}

	for rows.Next() {
		var o models.AdminOrg

		if err := rows.Scan(&o.ID, &o.Name, &o.Slug, &o.CreatedAt); err != nil {
			return nil, err
		}

		orgs = append(orgs, o)
	}

	return orgs, rows.Err()
}

// CountOrgs counts orgs whose name or slug contains query.
func (*AdminStore) CountOrgs(ctx *gofr.Context, query string) (int64, error) {
	pattern := likePattern(query)

	return countQuery(ctx, countOrgsQuery, pattern, pattern)
}

// OrgCounts returns the aggregate counts for the given orgs in four grouped
// queries (members, links, clicks since the bound timestamp, domains) —
// never one query per org. clicksSince binds as a UTC DATETIME string; the
// clicks count is served by the (org_id, ts) index.
func (*AdminStore) OrgCounts(ctx *gofr.Context, orgIDs []int64, clicksSince string,
) (map[int64]models.AdminOrgCounts, error) {
	counts := map[int64]models.AdminOrgCounts{}
	if len(orgIDs) == 0 {
		return counts, nil
	}

	assign := []struct {
		query string
		set   func(c *models.AdminOrgCounts, n int64)
		extra []any
	}{
		{query: orgMemberCounts, set: func(c *models.AdminOrgCounts, n int64) { c.Members = n }},
		{query: orgLinkCounts, set: func(c *models.AdminOrgCounts, n int64) { c.Links = n }},
		{query: orgClickCounts, set: func(c *models.AdminOrgCounts, n int64) { c.Clicks30d = n }, extra: []any{clicksSince}},
		{query: orgDomainCounts, set: func(c *models.AdminOrgCounts, n int64) { c.Domains = n }},
	}

	for _, a := range assign {
		query, args := inQuery(a.query, orgIDs)
		args = append(args, a.extra...)

		if err := scanGroupedCounts(ctx, query, args, counts, a.set); err != nil {
			return nil, err
		}
	}

	return counts, nil
}

// OrgOwners returns each org's first OWNER by join time in one grouped
// window-function query (no per-org N+1), keyed by org id. Ownerless orgs
// are simply absent.
func (*AdminStore) OrgOwners(ctx *gofr.Context, orgIDs []int64) (map[int64]models.AdminOrgOwner, error) {
	owners := map[int64]models.AdminOrgOwner{}
	if len(orgIDs) == 0 {
		return owners, nil
	}

	query, args := inQuery(orgOwnersQuery, orgIDs)

	rows, err := ctx.SQL.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var (
			orgID int64
			owner models.AdminOrgOwner
		)

		if err := rows.Scan(&orgID, &owner.ID, &owner.Email, &owner.Name); err != nil {
			return nil, err
		}

		owners[orgID] = owner
	}

	return owners, rows.Err()
}

// OrgExists reports whether the org id exists (the 404 check in front of the
// org member listing).
func (*AdminStore) OrgExists(ctx *gofr.Context, id int64) (bool, error) {
	n, err := countQuery(ctx, orgExistsQuery, id)

	return n > 0, err
}

// OrgUsers returns every member of one org joined with their user row,
// OWNERs first then join order.
func (*AdminStore) OrgUsers(ctx *gofr.Context, orgID int64) ([]models.AdminOrgUser, error) {
	rows, err := ctx.SQL.QueryContext(ctx, orgUsersQuery, orgID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	users := []models.AdminOrgUser{}

	for rows.Next() {
		var u models.AdminOrgUser

		if err := rows.Scan(&u.ID, &u.Email, &u.Name, &u.Role, &u.JoinedAt, &u.LastActiveAt); err != nil {
			return nil, err
		}

		users = append(users, u)
	}

	return users, rows.Err()
}

// ListReports returns one page of abuse reports, newest first, joined with
// each reported link's live status and destination.
func (*AdminStore) ListReports(ctx *gofr.Context, limit, offset int) ([]models.AdminReport, error) {
	rows, err := ctx.SQL.QueryContext(ctx, listReportsQuery, limit, offset)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	reports := []models.AdminReport{}

	for rows.Next() {
		var r models.AdminReport

		if err := rows.Scan(&r.ID, &r.Code, &r.LinkID, &r.OrgID, &r.Reason, &r.ReporterContact,
			&r.CreatedAt, &r.LinkStatus, &r.DestinationURL); err != nil {
			return nil, err
		}

		reports = append(reports, r)
	}

	return reports, rows.Err()
}

// CountReports counts all abuse reports.
func (*AdminStore) CountReports(ctx *gofr.Context) (int64, error) {
	return countQuery(ctx, countReportsQuery)
}

// GetLink fetches the operator view of one link by id (NOT org-scoped — the
// admin exception); (nil, nil) when absent.
func (*AdminStore) GetLink(ctx *gofr.Context, id int64) (*models.AdminLinkDetail, error) {
	var d models.AdminLinkDetail

	err := ctx.SQL.QueryRowContext(ctx, adminLinkQuery, id).Scan(&d.ID, &d.OrgID, &d.Code,
		&d.DestinationURL, &d.Status, &d.CreatedAt, &d.Visits, &d.OrgName, &d.CreatorEmail)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}

	if err != nil {
		return nil, err
	}

	return &d, nil
}

// SignupsPerDay returns users created per UTC day since the bound date
// (days with signups only; the service zero-fills).
func (*AdminStore) SignupsPerDay(ctx *gofr.Context, since string) ([]models.DayCount, error) {
	return dayCountQuery(ctx, signupsPerDayQuery, since)
}

// LinksCreatedPerDay returns links created per UTC day since the bound date.
func (*AdminStore) LinksCreatedPerDay(ctx *gofr.Context, since string) ([]models.DayCount, error) {
	return dayCountQuery(ctx, linksCreatedPerDayQuery, since)
}

// ClicksPerDay returns clicks recorded per UTC day since the bound date.
func (*AdminStore) ClicksPerDay(ctx *gofr.Context, since string) ([]models.DayCount, error) {
	return dayCountQuery(ctx, clicksPerDayAdminQuery, since)
}

// inQuery expands the %s IN-list hole of shape with one placeholder per id
// and returns the query plus the id args.
func inQuery(shape string, ids []int64) (string, []any) {
	placeholders := strings.TrimSuffix(strings.Repeat("?, ", len(ids)), ", ")

	args := make([]any, 0, len(ids))
	for _, id := range ids {
		args = append(args, id)
	}

	return strings.Replace(shape, "%s", placeholders, 1), args
}

// scanGroupedCounts runs one (group id, count) query and folds the counts
// into the map via set.
func scanGroupedCounts(ctx *gofr.Context, query string, args []any,
	counts map[int64]models.AdminOrgCounts, set func(*models.AdminOrgCounts, int64)) error {
	rows, err := ctx.SQL.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var (
			id int64
			n  int64
		)

		if err := rows.Scan(&id, &n); err != nil {
			return err
		}

		c := counts[id]
		set(&c, n)
		counts[id] = c
	}

	return rows.Err()
}

// dayCountQuery scans (date, count) rows.
func dayCountQuery(ctx *gofr.Context, query string, since string) ([]models.DayCount, error) {
	rows, err := ctx.SQL.QueryContext(ctx, query, since)
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
