package models

import "time"

// Instance-admin (operator) read models — the /api/internal/* API
// (ARCHITECTURE.md "Instance admin API"). These are DELIBERATELY cross-org:
// the instance admin API is the sanctioned exception to INV-6, gated by the
// deployment's ADMIN_API_TOKEN instead of an org-scoped session.

// AdminUser is one user as listed to the instance operator, with every org
// membership attached.
type AdminUser struct {
	ID        int64          `json:"id"`
	Email     string         `json:"email"`
	Name      string         `json:"name"`
	CreatedAt time.Time      `json:"created_at"`
	Orgs      []AdminUserOrg `json:"orgs"`
}

// AdminUserOrg is one org membership on an AdminUser row.
type AdminUserOrg struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Role string `json:"role"`
}

// AdminOrg is one org as listed to the instance operator, with cheap
// aggregate counts (members, links, clicks in the last 30 days, domains).
type AdminOrg struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Slug      string    `json:"slug"`
	CreatedAt time.Time `json:"created_at"`
	Members   int64     `json:"members"`
	Links     int64     `json:"links"`
	Clicks30d int64     `json:"clicks_30d"`
	Domains   int64     `json:"domains"`
}

// AdminOrgCounts carries the grouped aggregate counts for one org (filled
// into AdminOrg by the service).
type AdminOrgCounts struct {
	Members   int64
	Links     int64
	Clicks30d int64
	Domains   int64
}

// AdminReport is one abuse report as listed to the instance operator: the
// stored report row joined with the reported link's live status and
// destination, so triage needs no second lookup.
type AdminReport struct {
	ID              int64     `json:"id"`
	Code            string    `json:"code"`
	LinkID          int64     `json:"link_id"`
	OrgID           int64     `json:"org_id"`
	Reason          string    `json:"reason"`
	ReporterContact *string   `json:"reporter_contact"`
	CreatedAt       time.Time `json:"created_at"`
	LinkStatus      string    `json:"link_status"`
	DestinationURL  string    `json:"destination_url"`
}

// AdminLinkDetail is the operator view of one link: the row plus its org's
// name and its creator's email (nil for API-key-created links, which have no
// user).
type AdminLinkDetail struct {
	ID             int64     `json:"id"`
	OrgID          int64     `json:"org_id"`
	Code           string    `json:"code"`
	DestinationURL string    `json:"destination_url"`
	Status         string    `json:"status"`
	CreatedAt      time.Time `json:"created_at"`
	Visits         int64     `json:"visits"`
	OrgName        string    `json:"org_name"`
	CreatorEmail   *string   `json:"creator_email"`
}

// AdminDayStat is one day of instance-wide activity (UTC calendar day).
type AdminDayStat struct {
	Date         string `json:"date"`
	Signups      int64  `json:"signups"`
	LinksCreated int64  `json:"links_created"`
	Clicks       int64  `json:"clicks"`
}
