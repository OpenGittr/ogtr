// Package models holds the shared domain types for backend.
package models

import "time"

// Role values for org membership.
const (
	RoleOwner  = "OWNER"
	RoleMember = "MEMBER"
)

// User status values.
const (
	UserStatusEnabled = "ENABLED"
)

// Invite status values.
const (
	InviteStatusPending  = "PENDING"
	InviteStatusAccepted = "ACCEPTED"
	InviteStatusRevoked  = "REVOKED"
)

// User is a row in the users table.
type User struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Email     string    `json:"email"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

// Org is a row in the orgs table.
type Org struct {
	ID             int64     `json:"id"`
	Name           string    `json:"name"`
	Slug           string    `json:"slug"`
	AutoJoinDomain *string   `json:"auto_join_domain"`
	CreatedAt      time.Time `json:"created_at"`
}

// OrgMembership is one org a user belongs to, with their role in it.
type OrgMembership struct {
	OrgID int64  `json:"org_id"`
	Name  string `json:"name"`
	Slug  string `json:"slug"`
	Role  string `json:"role"`
}

// Member is one user inside an org, as listed to other org members.
type Member struct {
	UserID   int64     `json:"user_id"`
	Name     string    `json:"name"`
	Email    string    `json:"email"`
	Role     string    `json:"role"`
	JoinedAt time.Time `json:"joined_at"`
}

// Invite is a row in the invites table.
type Invite struct {
	ID        int64     `json:"id"`
	OrgID     int64     `json:"org_id"`
	Email     string    `json:"email"`
	InvitedBy int64     `json:"invited_by"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

// TokenPair is a ogtr access + refresh token set.
type TokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

// Link visibility types. PRIVATE links are listed only to their creator.
const (
	LinkTypePublic  = "PUBLIC"
	LinkTypePrivate = "PRIVATE"
)

// Link status values. DISABLED_ABUSE is set by the periodic destination
// re-scan when a live link's destination turns up flagged; a disabled link
// stops redirecting (410) but keeps its row, code and analytics. Re-enabling
// is a deliberate operator action (UPDATE links SET status='ACTIVE' ...) —
// there is no API for it.
const (
	LinkStatusActive        = "ACTIVE"
	LinkStatusDisabledAbuse = "DISABLED_ABUSE"
)

// API key status values. Keys are never hard-deleted (attribution history);
// "delete" sets DISABLED.
const (
	APIKeyStatusEnabled  = "ENABLED"
	APIKeyStatusDisabled = "DISABLED"
)

// APIKey is a row in the api_keys table. The full key ("slk_" + 40 random
// base62 chars) is returned exactly once at creation and stored only as a
// SHA-256 hex digest in key_hash — the hash never leaves the store layer.
// KeyHint is the recognizable prefix ("slk_" + first 8 random chars) shown in
// the list UI.
type APIKey struct {
	ID         int64      `json:"id"`
	OrgID      int64      `json:"org_id"`
	Name       string     `json:"name"`
	KeyHint    string     `json:"key_hint"`
	Status     string     `json:"status"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
}

// Link is a row in the links table. Code is the single short-code namespace
// shared by generated codes and custom aliases (FEATURES.md INV-5). ShortURL
// is computed from config (SHORT_SCHEME://SHORT_DOMAIN/code), never stored.
// UserID is nil for links created via a developer API key (which set APIKeyID
// instead); such links are always PUBLIC.
type Link struct {
	ID             int64           `json:"id"`
	OrgID          int64           `json:"org_id"`
	UserID         *int64          `json:"user_id"`
	APIKeyID       *int64          `json:"api_key_id,omitempty"`
	Code           string          `json:"code"`
	DestinationURL string          `json:"destination_url"`
	Type           string          `json:"type"`
	UTMSource      *string         `json:"utm_source,omitempty"`
	UTMMedium      *string         `json:"utm_medium,omitempty"`
	UTMCampaign    *string         `json:"utm_campaign,omitempty"`
	Status         string          `json:"status"`
	Deeplink       *DeeplinkConfig `json:"deeplink_config"`
	Visits         int64           `json:"visits"`
	LastVisitAt    *time.Time      `json:"last_visit_at"`
	CreatedAt      time.Time       `json:"created_at"`
	ShortURL       string          `json:"short_url"`
}

// AbuseReport is a row in the abuse_reports table: one visitor-submitted
// report against a short link (public POST /api/v1/report, rate-limited).
// OrgID/LinkID are derived from the reported code server-side; the reporter
// only ever supplies code, reason and an optional contact.
type AbuseReport struct {
	ID              int64     `json:"id"`
	OrgID           int64     `json:"org_id"`
	LinkID          int64     `json:"link_id"`
	Code            string    `json:"code"`
	Reason          string    `json:"reason"`
	ReporterContact *string   `json:"reporter_contact,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
}

// LinkEdit is one destination change of a link (link_edits table) — the
// audit trail behind PATCH /api/v1/links/{id}. Written on every successful
// edit; enables future analytics annotations ("destination changed here").
type LinkEdit struct {
	ID        int64     `json:"id"`
	OrgID     int64     `json:"org_id"`
	LinkID    int64     `json:"link_id"`
	UserID    int64     `json:"user_id"`
	OldURL    string    `json:"old_url"`
	NewURL    string    `json:"new_url"`
	CreatedAt time.Time `json:"created_at"`
}

// DeeplinkConfig is the owner-set mobile deep-link metadata stored in
// links.deeplink_config (FEATURES.md §3.2). Both platforms are optional;
// resolution reads this but never writes it (INV-3).
type DeeplinkConfig struct {
	Android *AndroidDeeplink `json:"android,omitempty"`
	IOS     *IOSDeeplink     `json:"ios,omitempty"`
}

// AndroidDeeplink builds the Android intent URI served to Android visitors:
// intent:{intent}#Intent;package={p};scheme={s};S.browser_fallback_url={f};end;
type AndroidDeeplink struct {
	Intent      string `json:"intent"`
	Package     string `json:"package"`
	Scheme      string `json:"scheme"`
	FallbackURL string `json:"fallback_url"`
}

// IOSDeeplink is the app link served to iOS visitors.
type IOSDeeplink struct {
	Intent string `json:"intent"`
}

// Empty reports whether the config carries no platform at all (an empty
// config is stored as NULL — "no deep links").
func (c *DeeplinkConfig) Empty() bool {
	return c == nil || (c.Android == nil && c.IOS == nil)
}

// Custom-domain status values. A domain serves redirects (and becomes
// eligible for primary display) only once VERIFIED.
const (
	DomainStatusPending  = "PENDING"
	DomainStatusVerified = "VERIFIED"
	DomainStatusDisabled = "DISABLED"
)

// Domain is a row in the domains table: an org-owned custom hostname for
// short links (FEATURES.md §1.6). Hostname is stored lowercase and
// punycode-normalized and is unique across the deployment — one org owns a
// hostname. TXTRecordName/TXTRecordValue are computed (never stored): the DNS
// TXT record the org must publish to prove control of the hostname.
type Domain struct {
	ID                int64      `json:"id"`
	OrgID             int64      `json:"org_id"`
	Hostname          string     `json:"hostname"`
	VerificationToken string     `json:"-"`
	Status            string     `json:"status"`
	VerifiedAt        *time.Time `json:"verified_at"`
	IsPrimary         bool       `json:"is_primary"`
	CreatedAt         time.Time  `json:"created_at"`
	TXTRecordName     string     `json:"txt_record_name"`
	TXTRecordValue    string     `json:"txt_record_value"`
}

// Rule condition operators (FEATURES.md §3.1).
const (
	ConditionIs    = "is"
	ConditionIsNot = "is_not"
)

// RuleCondition is one is/is_not condition over a list of values,
// case-insensitively matched.
type RuleCondition struct {
	Type   string   `json:"type"`
	Values []string `json:"values"`
}

// Rule is a row in link_rules: a conditional redirect evaluated in creation
// order on resolution — the first rule whose present conditions ALL match
// wins (FEATURES.md §3.1). DeviceType compares the visitor's mobile OS;
// Location compares the visitor's GeoIP city.
type Rule struct {
	ID         int64          `json:"id"`
	OrgID      int64          `json:"org_id"`
	LinkID     int64          `json:"link_id"`
	TargetName string         `json:"target_name"`
	DeviceType *RuleCondition `json:"device_type,omitempty"`
	Location   *RuleCondition `json:"location,omitempty"`
	URL        string         `json:"url"`
	CreatedAt  time.Time      `json:"created_at"`
}

// Click is one recorded resolution of a link (clicks table). A row is written
// for every resolution (FEATURES.md INV-4); ts defaults in the database.
type Click struct {
	OrgID         int64
	LinkID        int64
	UTMSource     string
	UTMMedium     string
	UTMCampaign   string
	DeviceType    string
	MobileOS      string
	Browser       string
	Referrer      string
	IP            string
	City          string
	Region        string
	Country       string
	IsDeeplink    bool
	TargetMatched bool
	CustomTagID   string
}
