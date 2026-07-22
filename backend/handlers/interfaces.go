// Package handlers maps HTTP requests onto the service layer.
package handlers

import (
	"gofr.dev/pkg/gofr"

	"github.com/opengittr/ogtr/backend/auth"
	"github.com/opengittr/ogtr/backend/models"
	"github.com/opengittr/ogtr/backend/services"
	"github.com/opengittr/ogtr/backend/visitor"
)

//go:generate mockgen -source=interfaces.go -destination=mock_interfaces.go -package=handlers

// AuthService is the auth/session dependency of the handlers.
type AuthService interface {
	Login(ctx *gofr.Context, provider, credential string) (*services.AuthResult, error)
	Refresh(ctx *gofr.Context, refreshToken string) (models.TokenPair, error)
	Me(ctx *gofr.Context, claims *auth.SessionClaims) (*services.MeResult, error)
	SwitchOrg(ctx *gofr.Context, userID, orgID int64) (models.TokenPair, error)
}

// LinkService is the link-management dependency of the handlers.
type LinkService interface {
	Shorten(ctx *gofr.Context, orgID, userID int64, in services.ShortenInput) (*models.Link, error)
	ShortenViaAPIKey(ctx *gofr.Context, orgID, apiKeyID int64, in services.ShortenInput) (*models.Link, error)
	Get(ctx *gofr.Context, orgID, viewerID, id int64) (*models.Link, error)
	List(ctx *gofr.Context, orgID, viewerID int64, page int) (*services.LinkPage, error)
	SetAlias(ctx *gofr.Context, orgID, viewerID, id int64, alias string) (*models.Link, error)
	SetDeeplink(ctx *gofr.Context, orgID, viewerID, id int64, cfg *models.DeeplinkConfig) (*models.Link, error)
	UpdateDestination(ctx *gofr.Context, orgID, actorID, id int64, in services.EditInput) (*models.Link, error)
	QRCodePNG(ctx *gofr.Context, orgID, viewerID, id int64) ([]byte, error)
}

// APIKeyService is the developer-API-key dependency of the handlers: key
// management CRUD plus the X-API-Key authentication used by link creation
// and resolution.
type APIKeyService interface {
	Create(ctx *gofr.Context, orgID int64, name string) (*services.CreatedAPIKey, error)
	List(ctx *gofr.Context, orgID int64) ([]models.APIKey, error)
	Disable(ctx *gofr.Context, orgID, id int64) (*models.APIKey, error)
	Authenticate(ctx *gofr.Context, rawKey string) (*models.APIKey, error)
}

// RuleService is the target-rule dependency of the handlers.
type RuleService interface {
	Create(ctx *gofr.Context, orgID, viewerID, linkID int64, inputs []services.RuleInput) ([]models.Rule, error)
	List(ctx *gofr.Context, orgID, viewerID, linkID int64) ([]models.Rule, error)
	Update(ctx *gofr.Context, orgID, viewerID, ruleID int64, in services.RuleInput) (*models.Rule, error)
	Delete(ctx *gofr.Context, orgID, viewerID, ruleID int64) error
}

// CityIndex is the city-autocomplete dependency (geo.Cities).
type CityIndex interface {
	Enabled() bool
	Search(prefix string, limit int) []string
}

// ResolveService is the resolution dependency of the redirect/resolve
// handlers. PreviewByCode backs GET /{code}+ (no click recorded).
type ResolveService interface {
	Resolve(ctx *gofr.Context, code, tag string, env visitor.Env) (*services.Resolution, error)
	PreviewByCode(ctx *gofr.Context, code string, env visitor.Env) (*services.Preview, error)
}

// ReportService is the public abuse-reporting dependency of the handlers.
type ReportService interface {
	Create(ctx *gofr.Context, in services.ReportInput, reporterIP string) (*models.AbuseReport, error)
}

// StatsService is the analytics dependency of the handlers.
type StatsService interface {
	LinkReport(ctx *gofr.Context, orgID, viewerID, linkID int64, from, to string, deeplink bool) (*models.LinkStatsReport, error)
	UniqueClicks(ctx *gofr.Context, orgID int64, linkIDs []int64) (*models.UniqueClicksResult, error)
	Tags(ctx *gofr.Context, orgID int64) ([]string, error)
	UTMAnalysis(ctx *gofr.Context, orgID, viewerID int64) (*models.UTMAnalysis, error)
}

// AdminService is the instance-admin dependency of the handlers: the
// operator API behind /api/internal/* — deliberately cross-org (the
// sanctioned INV-6 exception), served only by the separate backend/admin
// service behind its ADMIN_API_TOKEN gate.
type AdminService interface {
	Users(ctx *gofr.Context, query string, page int) (*services.AdminUsersPage, error)
	Orgs(ctx *gofr.Context, query string, page int) (*services.AdminOrgsPage, error)
	OrgUsers(ctx *gofr.Context, orgID int64) (*services.AdminOrgUsersPage, error)
	Reports(ctx *gofr.Context, page int) (*services.AdminReportsPage, error)
	Link(ctx *gofr.Context, id int64) (*models.AdminLinkDetail, error)
	DisableLink(ctx *gofr.Context, id int64, reason string) (*models.AdminLinkDetail, error)
	EnableLink(ctx *gofr.Context, id int64) (*models.AdminLinkDetail, error)
	DailyStats(ctx *gofr.Context, days int) (*services.AdminDailyStats, error)
}

// DomainService is the custom-domain dependency of the handlers. Mutations
// are OWNER-only (enforced in the service against the DB role); listing is
// open to all org members.
type DomainService interface {
	Create(ctx *gofr.Context, orgID, actorID int64, hostname string) (*models.Domain, error)
	List(ctx *gofr.Context, orgID int64) ([]models.Domain, error)
	Verify(ctx *gofr.Context, orgID, actorID, id int64) (*models.Domain, error)
	SetPrimary(ctx *gofr.Context, orgID, actorID, id int64) (*models.Domain, error)
	Delete(ctx *gofr.Context, orgID, actorID, id int64) error
}

// OrgService is the org-management dependency of the handlers.
type OrgService interface {
	Create(ctx *gofr.Context, creatorID int64, name, autoJoinDomain string) (*models.Org, error)
	Get(ctx *gofr.Context, orgID int64) (*models.Org, error)
	Update(ctx *gofr.Context, orgID, actorID int64, patch services.OrgUpdate) (*models.Org, error)
	Members(ctx *gofr.Context, orgID int64) ([]models.Member, error)
	RemoveMember(ctx *gofr.Context, orgID, actorID, targetID int64) error
	CreateInvite(ctx *gofr.Context, orgID, actorID int64, email string) (*models.Invite, error)
	ListInvites(ctx *gofr.Context, orgID int64) ([]models.Invite, error)
	RevokeInvite(ctx *gofr.Context, orgID, actorID, inviteID int64) error
}
