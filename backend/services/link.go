package services

import (
	"crypto/rand"
	"net/url"
	"regexp"
	"strings"

	qrcode "github.com/skip2/go-qrcode"
	"gofr.dev/pkg/gofr"

	"github.com/opengittr/ogtr/backend/apierrors"
	"github.com/opengittr/ogtr/backend/limits"
	"github.com/opengittr/ogtr/backend/models"
)

const (
	codeAlphabet    = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	codeLength      = 7
	maxCodeAttempts = 5

	linksPerPage = 10
	qrImageSize  = 512
)

// aliasRe is the custom-alias format (FEATURES.md §1.2).
var aliasRe = regexp.MustCompile(`^[a-zA-Z0-9_-]{3,50}$`)

// ShortenInput is the POST /api/v1/links payload after binding.
type ShortenInput struct {
	URL         string `json:"url"`
	Type        string `json:"type"`
	UTMSource   string `json:"utm_source"`
	UTMMedium   string `json:"utm_medium"`
	UTMCampaign string `json:"utm_campaign"`
}

// LinkPage is one page of the org's links.
type LinkPage struct {
	Links   []models.Link `json:"links"`
	Page    int           `json:"page"`
	PerPage int           `json:"per_page"`
	Total   int64         `json:"total"`
}

// LinkService implements link management: shorten, list, alias, QR,
// destination editing. members backs the org-OWNER permission check on
// destination edits (checked against the DB role, never the token claim);
// domains backs the primary-custom-domain short-URL display (FEATURES.md
// §1.6).
type LinkService struct {
	links        LinkStore
	members      MemberStore
	domains      DomainStore
	scanner      URLScanner
	policy       limits.Policy
	reserved     *ReservedAliases
	shortScheme  string
	shortDomain  string
	abuseContact string
}

// NewLinkService wires a LinkService. shortScheme/shortDomain come from
// SHORT_SCHEME / SHORT_DOMAIN config and build every short URL — unless the
// org has a primary VERIFIED custom domain, which then becomes the display
// base. scanner vets every destination (creation + edits); policy bounds link
// creation (wire limits.Unlimited{} unless the deployment supplies its own);
// reserved is the alias blacklist (nil means the built-in categories only);
// abuseContact (ABUSE_CONTACT config, may be empty) is appended to
// flagged-destination errors so a false positive has an escalation path.
func NewLinkService(links LinkStore, members MemberStore, domains DomainStore,
	urlScanner URLScanner, policy limits.Policy, reserved *ReservedAliases,
	shortScheme, shortDomain, abuseContact string) *LinkService {
	if reserved == nil {
		reserved = NewReservedAliases(nil)
	}

	return &LinkService{
		links: links, members: members, domains: domains,
		scanner: urlScanner, policy: policy, reserved: reserved,
		shortScheme: shortScheme, shortDomain: shortDomain, abuseContact: abuseContact,
	}
}

// checkDestination runs the URL scanner over a destination; a flagged
// verdict is a 422 whose message never reveals which list or rule matched
// (the coarse category is logged server-side only). A nil scanner allows
// everything (unit-test convenience); a scanner error fails open here
// because the pipeline's syntactic floor never errors.
func (s *LinkService) checkDestination(ctx *gofr.Context, orgID int64, destination string) error {
	if s.scanner == nil {
		return nil
	}

	verdict, err := s.scanner.Scan(ctx, destination)
	if err != nil {
		ctx.Logger.Errorf("destination scan failed (allowing): %v", err)

		return nil
	}

	if verdict.Allowed {
		return nil
	}

	ctx.Logger.Warnf("destination flagged (%s) in org %d: %s", verdict.Category, orgID, destination)

	msg := "This destination was flagged by security checks and can't be shortened."
	if s.abuseContact != "" {
		msg += " If you believe this is a mistake, contact " + s.abuseContact + "."
	}

	return apierrors.Unprocessable(msg)
}

// Shorten validates and normalizes the destination, dedupes per org, and
// creates a link with a random non-enumerable code. Shortening an existing
// destination — or a URL that is already a short link on this deployment —
// returns the existing link.
func (s *LinkService) Shorten(ctx *gofr.Context, orgID, userID int64, in ShortenInput) (*models.Link, error) {
	return s.shorten(ctx, orgID, &userID, nil, in)
}

// ShortenViaAPIKey is Shorten authenticated by a developer API key instead of
// a user session: the link records api_key_id and a NULL user_id. PRIVATE is
// rejected (422) — a private link needs a creator to be private to, so
// API-key-created links are always PUBLIC (ARCHITECTURE.md §4).
func (s *LinkService) ShortenViaAPIKey(ctx *gofr.Context, orgID, apiKeyID int64, in ShortenInput) (*models.Link, error) {
	return s.shorten(ctx, orgID, nil, &apiKeyID, in)
}

// shorten is the shared creation path. Exactly one of userID / apiKeyID is
// set; dedupe and already-short visibility use the creating user as viewer
// (an API key sees only PUBLIC links, viewer 0 matches no creator).
func (s *LinkService) shorten(ctx *gofr.Context, orgID int64, userID, apiKeyID *int64, in ShortenInput) (*models.Link, error) {
	linkType, err := normalizeLinkType(in.Type)
	if err != nil {
		return nil, err
	}

	if linkType == models.LinkTypePrivate && userID == nil {
		return nil, apierrors.Unprocessable("PRIVATE links need a signed-in creator; API keys can only create PUBLIC links")
	}

	var viewerID int64
	if userID != nil {
		viewerID = *userID
	}

	dest, err := normalizeURL(in.URL)
	if err != nil {
		return nil, err
	}

	if strings.EqualFold(dest.Host, s.shortDomain) {
		return s.existingShortURL(ctx, orgID, viewerID, dest)
	}

	// The deployment's limits.Policy gates creation on BOTH auth paths (JWT
	// and API key — this is their shared choke point) before any store
	// access; userID 0 marks the key path. Creation only: edits (alias,
	// deep link, destination) are never policy-checked, and neither is the
	// already-short echo above (it creates nothing).
	if err := s.policy.CanCreateLink(ctx, orgID, viewerID); err != nil {
		return nil, limitError(err)
	}

	// Scan before dedupe: a flagged destination is refused even when an
	// (older) identical link exists.
	if err := s.checkDestination(ctx, orgID, dest.String()); err != nil {
		return nil, err
	}

	utms := utmValues{
		source:   strings.TrimSpace(in.UTMSource),
		medium:   strings.TrimSpace(in.UTMMedium),
		campaign: strings.TrimSpace(in.UTMCampaign),
	}

	destination := appendUTMs(dest.String(), utms)

	if existing, err := s.links.FindByDestination(ctx, orgID, viewerID, destination); err != nil {
		return nil, err
	} else if existing != nil {
		return s.withShortURL(existing, s.displayBase(ctx, orgID)), nil
	}

	code, err := s.uniqueCode(ctx)
	if err != nil {
		return nil, err
	}

	link, err := s.links.Create(ctx, &models.Link{
		OrgID:          orgID,
		UserID:         userID,
		APIKeyID:       apiKeyID,
		Code:           code,
		DestinationURL: destination,
		Type:           linkType,
		UTMSource:      nilIfEmpty(utms.source),
		UTMMedium:      nilIfEmpty(utms.medium),
		UTMCampaign:    nilIfEmpty(utms.campaign),
	})
	if err != nil {
		return nil, err
	}

	if userID != nil {
		ctx.Logger.Infof("link %d (%s) created in org %d by user %d", link.ID, link.Code, orgID, *userID)
	} else {
		ctx.Logger.Infof("link %d (%s) created in org %d via api key %d", link.ID, link.Code, orgID, *apiKeyID)
	}

	return s.withShortURL(link, s.displayBase(ctx, orgID)), nil
}

// existingShortURL handles already-short detection (FEATURES.md §1.1): the
// submitted URL points at SHORT_DOMAIN, so return the link it refers to.
func (s *LinkService) existingShortURL(ctx *gofr.Context, orgID, viewerID int64, u *url.URL) (*models.Link, error) {
	code := strings.Trim(u.Path, "/")

	if code == "" || strings.Contains(code, "/") {
		return nil, apierrors.Unprocessable("url points at this short domain but is not a short link")
	}

	link, err := s.links.GetByCode(ctx, code)
	if err != nil {
		return nil, err
	}

	if link == nil || link.OrgID != orgID || !visibleTo(link, viewerID) {
		return nil, apierrors.Unprocessable("url points at this short domain but does not match one of your links")
	}

	return s.withShortURL(link, s.displayBase(ctx, orgID)), nil
}

// Get returns one link; org-scoped, PRIVATE links only to their creator.
func (s *LinkService) Get(ctx *gofr.Context, orgID, viewerID, id int64) (*models.Link, error) {
	link, err := s.links.GetByID(ctx, orgID, id)
	if err != nil {
		return nil, err
	}

	if link == nil || !visibleTo(link, viewerID) {
		return nil, apierrors.NotFound("link not found")
	}

	return s.withShortURL(link, s.displayBase(ctx, orgID)), nil
}

// List returns one page (10/page) of the org's links visible to the viewer.
func (s *LinkService) List(ctx *gofr.Context, orgID, viewerID int64, page int) (*LinkPage, error) {
	if page < 1 {
		page = 1
	}

	links, err := s.links.List(ctx, orgID, viewerID, linksPerPage, (page-1)*linksPerPage)
	if err != nil {
		return nil, err
	}

	total, err := s.links.Count(ctx, orgID, viewerID)
	if err != nil {
		return nil, err
	}

	base := s.displayBase(ctx, orgID) // one lookup for the whole page

	for i := range links {
		s.withShortURL(&links[i], base)
	}

	return &LinkPage{Links: links, Page: page, PerPage: linksPerPage, Total: total}, nil
}

// SetAlias replaces the link's code with a custom alias. Format violations
// and reserved words are 422; a taken code is 409. The old code stops
// resolving (documented behavior, ARCHITECTURE.md §2).
func (s *LinkService) SetAlias(ctx *gofr.Context, orgID, viewerID, id int64, alias string) (*models.Link, error) {
	alias = strings.TrimSpace(alias)

	if err := s.validateAlias(ctx, orgID, alias); err != nil {
		return nil, err
	}

	link, err := s.Get(ctx, orgID, viewerID, id)
	if err != nil {
		return nil, err
	}

	if link.Code == alias {
		return link, nil
	}

	taken, err := s.links.CodeExists(ctx, alias)
	if err != nil {
		return nil, err
	}

	if taken {
		return nil, apierrors.Conflict("this alias is already in use")
	}

	if err := s.links.UpdateCode(ctx, orgID, id, alias); err != nil {
		return nil, err
	}

	ctx.Logger.Infof("link %d alias set to %q in org %d by user %d (old code %q retired)",
		id, alias, orgID, viewerID, link.Code)

	return s.Get(ctx, orgID, viewerID, id)
}

// SetDeeplink replaces the link's owner-set deep-link config (FEATURES.md
// §3.2). A nil/empty config clears it. Visibility mirrors link detail:
// org-scoped, PRIVATE links writable only by their creator (a non-creator
// gets 404 — existence is not revealed). Resolution never calls this
// (INV-3: deep-link metadata is never visitor-writable).
func (s *LinkService) SetDeeplink(ctx *gofr.Context, orgID, viewerID, id int64, cfg *models.DeeplinkConfig) (*models.Link, error) {
	if err := validateDeeplink(cfg); err != nil {
		return nil, err
	}

	if _, err := s.Get(ctx, orgID, viewerID, id); err != nil {
		return nil, err
	}

	if err := s.links.UpdateDeeplink(ctx, orgID, id, cfg); err != nil {
		return nil, err
	}

	ctx.Logger.Infof("link %d deep-link config updated in org %d by user %d (cleared=%t)",
		id, orgID, viewerID, cfg.Empty())

	return s.Get(ctx, orgID, viewerID, id)
}

// validateDeeplink checks that every present platform has all its fields.
// An absent/empty config is valid — it means "clear".
func validateDeeplink(cfg *models.DeeplinkConfig) error {
	if cfg.Empty() {
		return nil
	}

	if a := cfg.Android; a != nil {
		if strings.TrimSpace(a.Intent) == "" || strings.TrimSpace(a.Package) == "" ||
			strings.TrimSpace(a.Scheme) == "" || strings.TrimSpace(a.FallbackURL) == "" {
			return apierrors.Unprocessable("android deep link needs intent, package, scheme and fallback_url")
		}
	}

	if i := cfg.IOS; i != nil && strings.TrimSpace(i.Intent) == "" {
		return apierrors.Unprocessable("ios deep link needs intent")
	}

	return nil
}

// EditInput is the PATCH /api/v1/links/{id} payload after binding: a new
// destination plus the UTM values to bake into it (same semantics as
// creation; empty UTMs mean "none").
type EditInput struct {
	URL         string `json:"url"`
	UTMSource   string `json:"utm_source"`
	UTMMedium   string `json:"utm_medium"`
	UTMCampaign string `json:"utm_campaign"`
}

// UpdateDestination repoints a link at a new destination (FEATURES.md §1.5):
// printed QR codes and published short links must stay useful when the target
// moves. Validation matches creation (scheme normalization, 422 on malformed,
// the own short domain rejected); dedupe stays creation-time-only. Permission:
// the link's creator or an org OWNER (DB role, like org management) — any
// other member gets 403. Visibility is checked first, so a cross-org id or
// another user's PRIVATE link stays a 404. Every applied edit writes a
// link_edits audit row.
func (s *LinkService) UpdateDestination(ctx *gofr.Context, orgID, actorID, id int64, in EditInput) (*models.Link, error) {
	link, err := s.links.GetByID(ctx, orgID, id)
	if err != nil {
		return nil, err
	}

	if link == nil || !visibleTo(link, actorID) {
		return nil, apierrors.NotFound("link not found")
	}

	if err := s.requireCreatorOrOwner(ctx, link, orgID, actorID); err != nil {
		return nil, err
	}

	dest, err := normalizeURL(in.URL)
	if err != nil {
		return nil, err
	}

	if strings.EqualFold(dest.Host, s.shortDomain) {
		return nil, apierrors.Unprocessable("destination must not point back at this short domain")
	}

	// Edits are scanned exactly like creation — repointing a clean link at a
	// flagged destination is the classic bait-and-switch.
	if err := s.checkDestination(ctx, orgID, dest.String()); err != nil {
		return nil, err
	}

	utms := utmValues{
		source:   strings.TrimSpace(in.UTMSource),
		medium:   strings.TrimSpace(in.UTMMedium),
		campaign: strings.TrimSpace(in.UTMCampaign),
	}

	destination := appendUTMs(dest.String(), utms)

	if destination == link.DestinationURL {
		return s.withShortURL(link, s.displayBase(ctx, orgID)), nil // no-op edit: nothing to write or audit
	}

	err = s.links.UpdateDestination(ctx, orgID, id, destination,
		nilIfEmpty(utms.source), nilIfEmpty(utms.medium), nilIfEmpty(utms.campaign))
	if err != nil {
		return nil, err
	}

	edit := &models.LinkEdit{
		OrgID:  orgID,
		LinkID: id,
		UserID: actorID,
		OldURL: link.DestinationURL,
		NewURL: destination,
	}
	if err := s.links.InsertEdit(ctx, edit); err != nil {
		return nil, err
	}

	ctx.Logger.Infof("link %d (%s) destination edited in org %d by user %d: %q -> %q",
		id, link.Code, orgID, actorID, link.DestinationURL, destination)

	return s.Get(ctx, orgID, actorID, id)
}

// requireCreatorOrOwner allows a destination edit by the link's creator or an
// org OWNER (role read from the DB, not the possibly-stale token claim, like
// org management does). API-key-created links have no creator, so only an
// OWNER may edit them.
func (s *LinkService) requireCreatorOrOwner(ctx *gofr.Context, link *models.Link, orgID, actorID int64) error {
	if link.UserID != nil && *link.UserID == actorID {
		return nil
	}

	role, err := s.members.GetRole(ctx, orgID, actorID)
	if err != nil {
		return err
	}

	if role != models.RoleOwner {
		return apierrors.Forbidden("only the link's creator or an org owner can edit its destination")
	}

	return nil
}

// QRCodePNG renders the link's short URL as a PNG QR code, generated on
// demand — never stored (ARCHITECTURE.md §2).
func (s *LinkService) QRCodePNG(ctx *gofr.Context, orgID, viewerID, id int64) ([]byte, error) {
	link, err := s.Get(ctx, orgID, viewerID, id)
	if err != nil {
		return nil, err
	}

	return qrcode.Encode(link.ShortURL, qrcode.Medium, qrImageSize)
}

// withShortURL fills the computed short URL onto a link from the org's
// display base (see displayBase).
func (*LinkService) withShortURL(link *models.Link, base string) *models.Link {
	link.ShortURL = base + "/" + link.Code

	return link
}

// displayBase returns the origin the org's short URLs are displayed (and
// QR-encoded) under: https://<primary verified custom domain> when the org
// has one, otherwise SHORT_SCHEME://SHORT_DOMAIN. Custom domains are always
// https — TLS for them is an operator/ingress concern (DEPLOYMENT.md). A
// lookup failure logs and falls back to the deployment domain: display must
// never block link management. Regardless of the display base, the
// SHORT_DOMAIN URL keeps resolving for every link (the code namespace stays
// global).
func (s *LinkService) displayBase(ctx *gofr.Context, orgID int64) string {
	host, err := s.domains.PrimaryVerifiedHostname(ctx, orgID)
	if err != nil {
		ctx.Logger.Errorf("primary domain lookup for org %d failed (falling back to %s): %v",
			orgID, s.shortDomain, err)

		host = ""
	}

	if host != "" {
		return "https://" + host
	}

	return s.shortScheme + "://" + s.shortDomain
}

// randomCodeFn generates candidate short codes; a package variable so the
// reserved-word retry is unit-testable (crypto/rand cannot be seeded).
var randomCodeFn = randomCode

// uniqueCode generates a random base62 code, retrying on collision — and on
// the (unlikely but real) draw of a reserved word, which must never become
// a code however it is produced. Generated codes live on the shared
// SHORT_DOMAIN, so the full reserved set applies.
func (s *LinkService) uniqueCode(ctx *gofr.Context) (string, error) {
	for range maxCodeAttempts {
		code, err := randomCodeFn(codeLength)
		if err != nil {
			return "", err
		}

		if s.reserved.IsReserved(code, false) {
			ctx.Logger.Infof("generated code %q is a reserved word, retrying", code)

			continue
		}

		taken, err := s.links.CodeExists(ctx, code)
		if err != nil {
			return "", err
		}

		if !taken {
			return code, nil
		}

		ctx.Logger.Infof("short-code collision on %q, retrying", code)
	}

	return "", apierrors.Conflict("could not generate a unique short code")
}

// randomCode returns n crypto/rand characters from the base62 alphabet,
// using rejection sampling to avoid modulo bias.
func randomCode(n int) (string, error) {
	const maxAcceptable = 248 // largest multiple of 62 below 256

	out := make([]byte, 0, n)
	buf := make([]byte, n*2)

	for len(out) < n {
		if _, err := rand.Read(buf); err != nil {
			return "", err
		}

		for _, b := range buf {
			if b < maxAcceptable && len(out) < n {
				out = append(out, codeAlphabet[int(b)%len(codeAlphabet)])
			}
		}
	}

	return string(out), nil
}

// visibleTo reports whether the viewer may see this link (PRIVATE links are
// creator-only, FEATURES.md §1.1). API-key-created links have no creator but
// are always PUBLIC, so they are visible to everyone in the org.
func visibleTo(link *models.Link, viewerID int64) bool {
	if link.Type != models.LinkTypePrivate {
		return true
	}

	return link.UserID != nil && *link.UserID == viewerID
}

// normalizeLinkType validates the optional link type; empty means PUBLIC.
func normalizeLinkType(t string) (string, error) {
	switch strings.ToUpper(strings.TrimSpace(t)) {
	case "", models.LinkTypePublic:
		return models.LinkTypePublic, nil
	case models.LinkTypePrivate:
		return models.LinkTypePrivate, nil
	default:
		return "", apierrors.Unprocessable("type must be PUBLIC or PRIVATE")
	}
}

// normalizeURL validates a destination URL, auto-prefixing https:// when no
// scheme is given (FEATURES.md §1.1 — https is the right default).
func normalizeURL(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, apierrors.Unprocessable("url must not be empty")
	}

	if !strings.Contains(raw, "://") {
		lower := strings.ToLower(raw)
		if strings.HasPrefix(lower, "http:") || strings.HasPrefix(lower, "https:") {
			return nil, apierrors.Unprocessable("url is malformed")
		}

		raw = "https://" + raw
	}

	u, err := url.Parse(raw)
	if err != nil {
		return nil, apierrors.Unprocessable("url is not a valid URL")
	}

	scheme := strings.ToLower(u.Scheme)
	if (scheme != "http" && scheme != "https") || u.Host == "" {
		return nil, apierrors.Unprocessable("url must be an http(s) URL with a host")
	}

	return u, nil
}

// validateAlias enforces the custom-alias format and the reserved-word
// blacklist (reserved.go). The reserved scope depends on the org: with a
// VERIFIED custom domain only the functional/infra category applies (the
// org owns its own namespace); otherwise the full curated set does. A
// failed domain lookup falls back to the strict full-list check — never the
// permissive one.
func (s *LinkService) validateAlias(ctx *gofr.Context, orgID int64, alias string) error {
	if !aliasRe.MatchString(alias) {
		return apierrors.Unprocessable("alias must be 3-50 characters of letters, digits, '_' or '-'")
	}

	if strings.HasPrefix(alias, ".") {
		return apierrors.Unprocessable("alias must not start with a dot")
	}

	customScope, err := s.domains.HasVerified(ctx, orgID)
	if err != nil {
		ctx.Logger.Errorf("verified-domain lookup for org %d failed (using strict reserved list): %v", orgID, err)

		customScope = false
	}

	if s.reserved.IsReserved(alias, customScope) {
		return apierrors.Unprocessable("this alias is reserved")
	}

	return nil
}

// utmValues are the three UTM parameters, explicit or auto-derived.
type utmValues struct {
	source, medium, campaign string
}

func (u utmValues) any() bool { return u.source != "" || u.medium != "" || u.campaign != "" }

// appendUTMs adds non-empty UTM values to a URL as proper query parameters
// (URL-encoded via net/url, preserving existing query and fragment — never
// string concatenation).
func appendUTMs(rawURL string, utms utmValues) string {
	if !utms.any() {
		return rawURL
	}

	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}

	q := u.Query()

	for key, val := range map[string]string{
		"utm_source":   utms.source,
		"utm_medium":   utms.medium,
		"utm_campaign": utms.campaign,
	} {
		if val != "" {
			q.Set(key, val)
		}
	}

	u.RawQuery = q.Encode()

	return u.String()
}

// nilIfEmpty maps "" to nil for nullable columns.
func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}

	return &s
}
