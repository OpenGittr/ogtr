// Package services holds the business logic between handlers and stores.
package services

import (
	"context"

	"gofr.dev/pkg/gofr"

	"github.com/opengittr/ogtr/backend/auth"
	"github.com/opengittr/ogtr/backend/geo"
	"github.com/opengittr/ogtr/backend/models"
	"github.com/opengittr/ogtr/backend/scanner"
)

//go:generate mockgen -source=interfaces.go -destination=mock_interfaces.go -package=services

// UserStore is the users data-access dependency.
type UserStore interface {
	GetByEmail(ctx *gofr.Context, email string) (*models.User, error)
	GetByID(ctx *gofr.Context, id int64) (*models.User, error)
	Create(ctx *gofr.Context, name, email string) (*models.User, error)
}

// OrgStore is the orgs data-access dependency.
type OrgStore interface {
	Create(ctx *gofr.Context, name, slug string, autoJoinDomain *string) (*models.Org, error)
	GetByID(ctx *gofr.Context, id int64) (*models.Org, error)
	SlugExists(ctx *gofr.Context, slug string) (bool, error)
	Update(ctx *gofr.Context, id int64, name string, autoJoinDomain *string) error
	GetByAutoJoinDomain(ctx *gofr.Context, domain string) (*models.Org, error)
}

// MemberStore is the org_members data-access dependency.
type MemberStore interface {
	GetRole(ctx *gofr.Context, orgID, userID int64) (string, error)
	Add(ctx *gofr.Context, orgID, userID int64, role string) error
	Remove(ctx *gofr.Context, orgID, userID int64) error
	CountOwners(ctx *gofr.Context, orgID int64) (int, error)
	ListOrgsForUser(ctx *gofr.Context, userID int64) ([]models.OrgMembership, error)
	ListMembers(ctx *gofr.Context, orgID int64) ([]models.Member, error)
}

// InviteStore is the invites data-access dependency.
type InviteStore interface {
	Create(ctx *gofr.Context, orgID int64, email string, invitedBy int64) (*models.Invite, error)
	GetByID(ctx *gofr.Context, orgID, id int64) (*models.Invite, error)
	ListPending(ctx *gofr.Context, orgID int64) ([]models.Invite, error)
	HasPending(ctx *gofr.Context, orgID int64, email string) (bool, error)
	PendingForEmail(ctx *gofr.Context, email string) ([]models.Invite, error)
	SetStatus(ctx *gofr.Context, id int64, status string) error
}

// TokenIssuer signs and parses ogtr session tokens.
type TokenIssuer interface {
	IssuePair(userID, orgID int64, role string) (models.TokenPair, error)
	Parse(raw, wantType string) (*auth.SessionClaims, error)
}

// LinkStore is the links data-access dependency.
type LinkStore interface {
	Create(ctx *gofr.Context, l *models.Link) (*models.Link, error)
	GetByID(ctx *gofr.Context, orgID, id int64) (*models.Link, error)
	GetByCode(ctx *gofr.Context, code string) (*models.Link, error)
	FindByDestination(ctx *gofr.Context, orgID, viewerID int64, destinationURL string) (*models.Link, error)
	CodeExists(ctx *gofr.Context, code string) (bool, error)
	List(ctx *gofr.Context, orgID, viewerID int64, limit, offset int) ([]models.Link, error)
	Count(ctx *gofr.Context, orgID, viewerID int64) (int64, error)
	UpdateCode(ctx *gofr.Context, orgID, id int64, code string) error
	UpdateDeeplink(ctx *gofr.Context, orgID, id int64, cfg *models.DeeplinkConfig) error
	UpdateDestination(ctx *gofr.Context, orgID, id int64, destinationURL string, utmSource, utmMedium, utmCampaign *string) error
	InsertEdit(ctx *gofr.Context, e *models.LinkEdit) error
	RecordVisit(ctx *gofr.Context, id int64) error
	// SetStatusByID / ListActiveClickedSince back the periodic destination
	// re-scan (a system job, not an authenticated action — see the store for
	// why these two are not org-scoped). since binds as a DATETIME string.
	SetStatusByID(ctx *gofr.Context, id int64, status string) error
	ListActiveClickedSince(ctx *gofr.Context, since string, afterID int64, limit int) ([]models.Link, error)
}

// URLScanner is the destination-scanning dependency (scanner.Pipeline in
// production): consulted on link creation, destination edits and the
// periodic re-scan. The verdict's category is coarse by design.
type URLScanner interface {
	Scan(ctx context.Context, rawURL string) (scanner.Verdict, error)
}

// AbuseReportStore is the abuse_reports data-access dependency.
type AbuseReportStore interface {
	Insert(ctx *gofr.Context, r *models.AbuseReport) error
}

// APIKeyStore is the api_keys data-access dependency. Key material never
// crosses this boundary except as a SHA-256 hex digest (and the display hint).
type APIKeyStore interface {
	Create(ctx *gofr.Context, orgID int64, name, keyHash, keyHint string) (*models.APIKey, error)
	GetByID(ctx *gofr.Context, orgID, id int64) (*models.APIKey, error)
	GetByHash(ctx *gofr.Context, keyHash string) (*models.APIKey, error)
	List(ctx *gofr.Context, orgID int64) ([]models.APIKey, error)
	Disable(ctx *gofr.Context, orgID, id int64) error
	TouchLastUsed(ctx *gofr.Context, id int64) error
}

// DomainStore is the domains data-access dependency (per-org custom short
// domains). GetByHostname is deliberately not org-scoped: hostnames are a
// deployment-wide namespace and the redirect path derives the org FROM the
// hostname.
type DomainStore interface {
	Create(ctx *gofr.Context, orgID int64, hostname, verificationToken string) (*models.Domain, error)
	GetByID(ctx *gofr.Context, orgID, id int64) (*models.Domain, error)
	GetByHostname(ctx *gofr.Context, hostname string) (*models.Domain, error)
	ListByOrg(ctx *gofr.Context, orgID int64) ([]models.Domain, error)
	PrimaryVerifiedHostname(ctx *gofr.Context, orgID int64) (string, error)
	// HasVerified reports whether the org owns any VERIFIED custom domain —
	// the trigger for the relaxed (functional-only) reserved-alias scope.
	HasVerified(ctx *gofr.Context, orgID int64) (bool, error)
	SetVerified(ctx *gofr.Context, orgID, id int64) error
	// SetPrimary transactionally swaps the org's single primary to this
	// domain; (false, nil) means the domain was not a VERIFIED row of this
	// org and nothing changed.
	SetPrimary(ctx *gofr.Context, orgID, id int64) (bool, error)
	Delete(ctx *gofr.Context, orgID, id int64) (bool, error)
}

// DNSResolver looks up DNS TXT records for domain-ownership verification.
// *net.Resolver satisfies it directly — production wires net.DefaultResolver
// and tests substitute a mock; the verification code path is identical in
// both (no bypass).
type DNSResolver interface {
	LookupTXT(ctx context.Context, name string) ([]string, error)
}

// ClickStore is the clicks data-access dependency.
type ClickStore interface {
	Insert(ctx *gofr.Context, c *models.Click) error
}

// StatsStore is the click-analytics data-access dependency. Date arguments
// bind as YYYY-MM-DD strings; the range includes both end days.
type StatsStore interface {
	TotalClicks(ctx *gofr.Context, orgID, linkID int64, from, to string) (int64, error)
	ClicksPerDay(ctx *gofr.Context, orgID, linkID int64, from, to string) ([]models.DayCount, error)
	BrowserPerDay(ctx *gofr.Context, orgID, linkID int64, from, to string) ([]models.DayDimCount, error)
	DevicePerDay(ctx *gofr.Context, orgID, linkID int64, from, to string) ([]models.DayDimCount, error)
	ReferrerPerDay(ctx *gofr.Context, orgID, linkID int64, from, to string) ([]models.DayDimCount, error)
	MobileOSPerDay(ctx *gofr.Context, orgID, linkID int64, from, to string) ([]models.DayDimCount, error)
	CountryPerDay(ctx *gofr.Context, orgID, linkID int64, from, to string) ([]models.DayDimCount, error)
	RegionPerDay(ctx *gofr.Context, orgID, linkID int64, from, to string) ([]models.DayDimCount, error)
	CityPerDay(ctx *gofr.Context, orgID, linkID int64, from, to string) ([]models.DayDimCount, error)
	BrowserTotals(ctx *gofr.Context, orgID, linkID int64, from, to string) ([]models.DimCount, error)
	DeviceTotals(ctx *gofr.Context, orgID, linkID int64, from, to string) ([]models.DimCount, error)
	ReferrerTotals(ctx *gofr.Context, orgID, linkID int64, from, to string) ([]models.DimCount, error)
	MobileOSTotals(ctx *gofr.Context, orgID, linkID int64, from, to string) ([]models.DimCount, error)
	CountryTotals(ctx *gofr.Context, orgID, linkID int64, from, to string) ([]models.DimCount, error)
	RegionTotals(ctx *gofr.Context, orgID, linkID int64, from, to string) ([]models.DimCount, error)
	CityTotals(ctx *gofr.Context, orgID, linkID int64, from, to string) ([]models.DimCount, error)
	DeeplinkClickCount(ctx *gofr.Context, orgID, linkID int64, from, to string, isDeeplink bool) (int64, error)
	TargetMatchedCount(ctx *gofr.Context, orgID, linkID int64, from, to string) (int64, error)
	ClickDetails(ctx *gofr.Context, orgID, linkID int64, from, to string) ([]models.ClickDetail, error)
	UniqueTagClicks(ctx *gofr.Context, orgID int64, linkIDs []int64) (int64, error)
	DistinctTags(ctx *gofr.Context, orgID int64) ([]string, error)
	UTMSourceCounts(ctx *gofr.Context, orgID, viewerID int64) ([]models.UTMCount, error)
	UTMMediumCounts(ctx *gofr.Context, orgID, viewerID int64) ([]models.UTMCount, error)
	UTMCampaignCounts(ctx *gofr.Context, orgID, viewerID int64) ([]models.UTMCount, error)
}

// RuleStore is the link_rules data-access dependency.
type RuleStore interface {
	Create(ctx *gofr.Context, r *models.Rule) (*models.Rule, error)
	GetByID(ctx *gofr.Context, orgID, id int64) (*models.Rule, error)
	ListByLink(ctx *gofr.Context, orgID, linkID int64) ([]models.Rule, error)
	Update(ctx *gofr.Context, r *models.Rule) (*models.Rule, error)
	Delete(ctx *gofr.Context, orgID, id int64) error
}

// LocationResolver resolves a visitor IP to its city/region/country in one
// lookup; the zero Location when GeoIP is disabled or the IP is unknown
// (geo.Locator).
type LocationResolver interface {
	LocationForIP(ip string) geo.Location
}
