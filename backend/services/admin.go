package services

import (
	"time"

	"gofr.dev/pkg/gofr"

	"github.com/opengittr/ogtr/backend/apierrors"
	"github.com/opengittr/ogtr/backend/models"
)

const (
	// adminPageSize is the fixed page size of every admin listing.
	adminPageSize = 25

	// adminClicksWindow is the orgs listing's clicks_30d window.
	adminClicksWindow = 30 * 24 * time.Hour

	// Daily-stats bounds: ?days defaults to 30 and is capped at 90.
	adminStatsDefaultDays = 30
	adminStatsMaxDays     = 90
)

// AdminUsersPage is one page of the instance-wide user listing.
type AdminUsersPage struct {
	Users []models.AdminUser `json:"users"`
	Total int64              `json:"total"`
}

// AdminOrgsPage is one page of the instance-wide org listing.
type AdminOrgsPage struct {
	Orgs  []models.AdminOrg `json:"orgs"`
	Total int64             `json:"total"`
}

// AdminOrgUsersPage is one org's full member list (OWNERs first).
type AdminOrgUsersPage struct {
	Users []models.AdminOrgUser `json:"users"`
}

// AdminReportsPage is one page of the abuse-report listing, newest first.
type AdminReportsPage struct {
	Reports []models.AdminReport `json:"reports"`
	Total   int64                `json:"total"`
}

// AdminDailyStats is the instance-wide per-day activity series.
type AdminDailyStats struct {
	Days []models.AdminDayStat `json:"days"`
}

// adminNowFn supplies the clock; a package variable so window math is
// unit-testable.
var adminNowFn = time.Now

// AdminService implements the instance-admin (operator) API behind
// /api/internal/* (ARCHITECTURE.md "Instance admin API"). Everything here is
// DELIBERATELY cross-org — the sanctioned exception to INV-6; the
// compensating control is the ADMIN_API_TOKEN gate in front of every route.
type AdminService struct {
	store AdminStore
	links LinkStatusStore
}

// NewAdminService wires an AdminService. links is the same store the
// re-scan's disable path uses, so operator disable/enable shares that status
// machinery exactly.
func NewAdminService(store AdminStore, links LinkStatusStore) *AdminService {
	return &AdminService{store: store, links: links}
}

// pageOffset converts a 1-based page into the SQL offset (pages below 1
// clamp to the first page).
func pageOffset(page int) int {
	if page < 1 {
		page = 1
	}

	return (page - 1) * adminPageSize
}

// Users returns one page (25/page) of users across every org, newest first,
// each with all its org memberships (one grouped query, no N+1). query
// matches email or name, case per the column collation.
func (s *AdminService) Users(ctx *gofr.Context, query string, page int) (*AdminUsersPage, error) {
	users, err := s.store.ListUsers(ctx, query, adminPageSize, pageOffset(page))
	if err != nil {
		return nil, err
	}

	total, err := s.store.CountUsers(ctx, query)
	if err != nil {
		return nil, err
	}

	ids := make([]int64, 0, len(users))
	for i := range users {
		ids = append(ids, users[i].ID)
	}

	memberships, err := s.store.UserOrgs(ctx, ids)
	if err != nil {
		return nil, err
	}

	for i := range users {
		if orgs, ok := memberships[users[i].ID]; ok {
			users[i].Orgs = orgs
		}
	}

	return &AdminUsersPage{Users: users, Total: total}, nil
}

// Orgs returns one page (25/page) of orgs, newest first, with aggregate
// counts (members, links, clicks in the last 30 days, domains) filled from
// grouped queries. query matches name or slug.
func (s *AdminService) Orgs(ctx *gofr.Context, query string, page int) (*AdminOrgsPage, error) {
	orgs, err := s.store.ListOrgs(ctx, query, adminPageSize, pageOffset(page))
	if err != nil {
		return nil, err
	}

	total, err := s.store.CountOrgs(ctx, query)
	if err != nil {
		return nil, err
	}

	ids := make([]int64, 0, len(orgs))
	for i := range orgs {
		ids = append(ids, orgs[i].ID)
	}

	since := adminNowFn().UTC().Add(-adminClicksWindow).Format("2006-01-02 15:04:05")

	counts, err := s.store.OrgCounts(ctx, ids, since)
	if err != nil {
		return nil, err
	}

	owners, err := s.store.OrgOwners(ctx, ids)
	if err != nil {
		return nil, err
	}

	for i := range orgs {
		c := counts[orgs[i].ID]
		orgs[i].Members = c.Members
		orgs[i].Links = c.Links
		orgs[i].Clicks30d = c.Clicks30d
		orgs[i].Domains = c.Domains

		if owner, ok := owners[orgs[i].ID]; ok {
			orgs[i].Owner = &owner
		}
	}

	return &AdminOrgsPage{Orgs: orgs, Total: total}, nil
}

// OrgUsers returns one org's full member list (no paging — org sizes are
// bounded by membership limits), OWNERs first then join order; unknown org
// ids are 404.
func (s *AdminService) OrgUsers(ctx *gofr.Context, orgID int64) (*AdminOrgUsersPage, error) {
	exists, err := s.store.OrgExists(ctx, orgID)
	if err != nil {
		return nil, err
	}

	if !exists {
		return nil, apierrors.NotFound("org not found")
	}

	users, err := s.store.OrgUsers(ctx, orgID)
	if err != nil {
		return nil, err
	}

	return &AdminOrgUsersPage{Users: users}, nil
}

// Reports returns one page (25/page) of abuse reports, newest first, each
// joined with the reported link's live status and destination.
func (s *AdminService) Reports(ctx *gofr.Context, page int) (*AdminReportsPage, error) {
	reports, err := s.store.ListReports(ctx, adminPageSize, pageOffset(page))
	if err != nil {
		return nil, err
	}

	total, err := s.store.CountReports(ctx)
	if err != nil {
		return nil, err
	}

	return &AdminReportsPage{Reports: reports, Total: total}, nil
}

// Link returns the operator view of one link (any org); unknown ids are 404.
func (s *AdminService) Link(ctx *gofr.Context, id int64) (*models.AdminLinkDetail, error) {
	link, err := s.store.GetLink(ctx, id)
	if err != nil {
		return nil, err
	}

	if link == nil {
		return nil, apierrors.NotFound("link not found")
	}

	return link, nil
}

// DisableLink flips a link to DISABLED_ABUSE — identical semantics to a
// re-scan disable: GET /{code} serves the 410 page, no clicks are recorded,
// the row/code/analytics survive. The action and its reason are logged
// (audit-light v1). Idempotent on an already-disabled link.
func (s *AdminService) DisableLink(ctx *gofr.Context, id int64, reason string) (*models.AdminLinkDetail, error) {
	return s.setStatus(ctx, id, models.LinkStatusDisabledAbuse, reason)
}

// EnableLink flips a link back to ACTIVE — the API form of the operator
// re-enable that used to be a manual DB action. Logged like DisableLink.
func (s *AdminService) EnableLink(ctx *gofr.Context, id int64) (*models.AdminLinkDetail, error) {
	return s.setStatus(ctx, id, models.LinkStatusActive, "")
}

func (s *AdminService) setStatus(ctx *gofr.Context, id int64, status, reason string) (*models.AdminLinkDetail, error) {
	link, err := s.Link(ctx, id)
	if err != nil {
		return nil, err
	}

	if err := s.links.SetStatusByID(ctx, id, status); err != nil {
		return nil, err
	}

	link.Status = status

	if reason == "" {
		reason = "none given"
	}

	ctx.Logger.Warnf("instance admin set link %d (%s) in org %d to %s — reason: %s",
		link.ID, link.Code, link.OrgID, status, reason)

	return link, nil
}

// DailyStats returns the instance-wide per-day activity series for the last
// `days` UTC calendar days including today (default 30, capped at 90),
// zero-filled so every day in the window is present, oldest first.
func (s *AdminService) DailyStats(ctx *gofr.Context, days int) (*AdminDailyStats, error) {
	if days < 1 {
		days = adminStatsDefaultDays
	}

	if days > adminStatsMaxDays {
		days = adminStatsMaxDays
	}

	today := adminNowFn().UTC().Truncate(24 * time.Hour)
	start := today.AddDate(0, 0, -(days - 1))
	since := start.Format("2006-01-02")

	signups, err := s.store.SignupsPerDay(ctx, since)
	if err != nil {
		return nil, err
	}

	links, err := s.store.LinksCreatedPerDay(ctx, since)
	if err != nil {
		return nil, err
	}

	clicks, err := s.store.ClicksPerDay(ctx, since)
	if err != nil {
		return nil, err
	}

	signupsByDay := dayCountMap(signups)
	linksByDay := dayCountMap(links)
	clicksByDay := dayCountMap(clicks)

	series := make([]models.AdminDayStat, 0, days)

	for d := 0; d < days; d++ {
		date := start.AddDate(0, 0, d).Format("2006-01-02")

		series = append(series, models.AdminDayStat{
			Date:         date,
			Signups:      signupsByDay[date],
			LinksCreated: linksByDay[date],
			Clicks:       clicksByDay[date],
		})
	}

	return &AdminDailyStats{Days: series}, nil
}

func dayCountMap(counts []models.DayCount) map[string]int64 {
	m := make(map[string]int64, len(counts))
	for _, c := range counts {
		m[c.Date] = c.Clicks
	}

	return m
}
