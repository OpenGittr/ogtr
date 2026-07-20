package services

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"gofr.dev/pkg/gofr"

	"github.com/opengittr/ogtr/backend/apierrors"
	"github.com/opengittr/ogtr/backend/models"
	"github.com/opengittr/ogtr/backend/ratelimit"
	"github.com/opengittr/ogtr/backend/visitor"
)

// Resolution is the outcome of resolving a short code: where to send the
// visitor. Served as JSON by GET /api/v1/resolve and as a 302 Location by
// GET /{code}.
type Resolution struct {
	Code string `json:"code"`
	URL  string `json:"url"`
}

// DisabledLinkError is returned when a resolved link has status
// DISABLED_ABUSE: HTTP 410 Gone — the code existed but was withdrawn. The
// redirect handler renders it as an HTML warning page; the JSON resolve
// endpoint serves it as a plain 410 error. AbuseContact carries the
// deployment's ABUSE_CONTACT (may be empty) for the appeal line.
type DisabledLinkError struct {
	Code         string
	AbuseContact string
}

// Error implements the error interface. Coarse by design: no category, no
// list, no rule.
func (*DisabledLinkError) Error() string {
	return "this link has been disabled after being flagged by security checks"
}

// StatusCode makes gofr answer 410 Gone.
func (*DisabledLinkError) StatusCode() int { return http.StatusGone }

// Preview is what GET /{code}+ shows a cautious visitor: the destination as
// text (never a styled call-to-action), plus whether the link was disabled.
// No click is recorded for a preview.
type Preview struct {
	Code           string
	DestinationURL string
	Disabled       bool
}

// ResolveService implements the shared resolution pipeline
// (ARCHITECTURE.md §4 "Resolution & redirect"). domains + shortDomain make
// it host-aware: on the deployment's own SHORT_DOMAIN (and local-dev
// loopback hosts) every org's code resolves (the global namespace), while a
// VERIFIED custom domain resolves only its owning org's links.
type ResolveService struct {
	links        LinkStore
	clicks       ClickStore
	rules        RuleStore
	domains      DomainStore
	geo          LocationResolver
	throttle     *ratelimit.GuessThrottle
	selfSource   string
	shortDomain  string
	abuseContact string
}

// NewResolveService wires a ResolveService. selfSource (UTM_SELF_SOURCE) is
// the brand name auto-tagged onto direct traffic; geo is the shared GeoIP
// locator (a disabled locator when GEOIP_DB_PATH is unset); shortDomain is
// the deployment's SHORT_DOMAIN for request-host classification. throttle
// (nil = disabled) is the anti-enumeration 404 guess throttle;
// abuseContact rides on disabled-link 410s.
func NewResolveService(links LinkStore, clicks ClickStore, rules RuleStore, domains DomainStore,
	geo LocationResolver, throttle *ratelimit.GuessThrottle,
	selfSource, shortDomain, abuseContact string) *ResolveService {
	return &ResolveService{
		links: links, clicks: clicks, rules: rules, domains: domains,
		geo: geo, throttle: throttle,
		selfSource: selfSource, shortDomain: shortDomain, abuseContact: abuseContact,
	}
}

// Resolve runs the pipeline: lookup → visitor location (GeoIP) → UTM tagging →
// deep link → target rules → counters + click record. Unknown codes are 404.
// Counter and click writes are best-effort: their failure is logged but never
// blocks the redirect.
//
// Deep link evaluates before target rules (FEATURES.md §2.1): a matching
// deep link swaps the outgoing URL for the app intent, and a matching target
// rule overrides that again — both flags are recorded on the click. Deep-link
// and rule URLs are served exactly as configured (no UTM auto-tagging); the
// click still records the UTM set of the default destination.
func (s *ResolveService) Resolve(ctx *gofr.Context, code, tag string, env visitor.Env) (*Resolution, error) {
	link, err := s.lookup(ctx, code, env)
	if err != nil {
		return nil, err
	}

	// A disabled link answers 410 Gone and records nothing: the redirect
	// handler turns this into the HTML warning page, the JSON endpoint
	// serves it as-is. Deliberately distinct from 404 — the code existed,
	// pretending otherwise would just invite re-shortening the destination.
	if link.Status == models.LinkStatusDisabledAbuse {
		return nil, &DisabledLinkError{Code: link.Code, AbuseContact: s.abuseContact}
	}

	// One GeoIP lookup per click: the city feeds target rules, the full
	// city/region/country set is recorded on the click for location analytics.
	loc := s.geo.LocationForIP(env.IP)

	outURL, utms := s.applyUTMs(link, env)

	isDeeplink := false
	if deeplink, ok := deeplinkURL(link.Deeplink, env.MobileOS); ok {
		outURL, isDeeplink = deeplink, true
	}

	targetMatched := false
	if ruleURL, ok := s.matchTargetRules(ctx, link, env.MobileOS, loc.City); ok {
		outURL, targetMatched = ruleURL, true
	}

	if err := s.links.RecordVisit(ctx, link.ID); err != nil {
		ctx.Logger.Errorf("recording visit for link %d failed: %v", link.ID, err)
	}

	click := models.Click{
		OrgID:         link.OrgID,
		LinkID:        link.ID,
		UTMSource:     utms.source,
		UTMMedium:     utms.medium,
		UTMCampaign:   utms.campaign,
		DeviceType:    env.DeviceType,
		MobileOS:      env.MobileOS,
		Browser:       env.Browser,
		Referrer:      env.Referrer,
		IP:            env.IP,
		City:          loc.City,
		Region:        loc.Region,
		Country:       loc.Country,
		IsDeeplink:    isDeeplink,
		TargetMatched: targetMatched,
		CustomTagID:   tag,
	}
	if err := s.clicks.Insert(ctx, &click); err != nil {
		ctx.Logger.Errorf("recording click for link %d failed: %v", link.ID, err)
	}

	return &Resolution{Code: link.Code, URL: outURL}, nil
}

// lookup is the shared host-scoped code lookup with the anti-enumeration
// guess throttle: an IP past its unknown-code budget gets 429 for a
// cooldown, and every unknown-code (or out-of-scope) 404 counts as a miss.
// Successful lookups never count.
func (s *ResolveService) lookup(ctx *gofr.Context, code string, env visitor.Env) (*models.Link, error) {
	if s.throttle != nil && s.throttle.Blocked(env.IP) {
		return nil, apierrors.TooManyRequests("too many requests — try again shortly")
	}

	scopeOrg, err := s.hostScope(ctx, env.Host)
	if err != nil {
		s.recordMissIf404(err, env.IP)

		return nil, err
	}

	link, err := s.links.GetByCode(ctx, code)
	if err != nil {
		return nil, err
	}

	// A custom domain resolves ONLY its owning org's links; another org's
	// code answers the same 404 as an unknown code (existence stays hidden).
	if link == nil || (scopeOrg != 0 && link.OrgID != scopeOrg) {
		if s.throttle != nil {
			s.throttle.RecordMiss(env.IP)
		}

		return nil, apierrors.NotFound("short link not found")
	}

	return link, nil
}

// recordMissIf404 counts host-scope 404s (unknown hostname, unverified
// domain) as guess misses too — they are the same enumeration surface.
func (s *ResolveService) recordMissIf404(err error, ip string) {
	if s.throttle == nil {
		return
	}

	var apiErr apierrors.Error
	if errors.As(err, &apiErr) && apiErr.StatusCode() == http.StatusNotFound {
		s.throttle.RecordMiss(ip)
	}
}

// PreviewByCode is the GET /{code}+ lookup (FEATURES-level: the link
// preview page). Same host scoping and guess throttling as resolution, but
// NO click is recorded, no counters move and the redirect pipeline (deep
// links, rules, UTM) never runs — the visitor is inspecting, not visiting.
// Disabled links still preview (with Disabled set) so the page can explain.
func (s *ResolveService) PreviewByCode(ctx *gofr.Context, code string, env visitor.Env) (*Preview, error) {
	link, err := s.lookup(ctx, code, env)
	if err != nil {
		return nil, err
	}

	return &Preview{
		Code:           link.Code,
		DestinationURL: link.DestinationURL,
		Disabled:       link.Status == models.LinkStatusDisabledAbuse,
	}, nil
}

// hostScope classifies the request Host (ARCHITECTURE.md §4 "Host-aware
// resolution"): the deployment's own short domain (or a local-dev loopback
// host, or no Host at all) keeps the global namespace (org 0 = unscoped); a
// VERIFIED custom domain scopes resolution to its owning org; anything else
// — unknown hostnames and PENDING/DISABLED domains — is 404. Unknown hosts
// deliberately never bounce to WEBSITE_URL: whatever DNS someone points at
// this deployment must not become a marketing redirect.
func (s *ResolveService) hostScope(ctx *gofr.Context, host string) (int64, error) {
	if IsDeploymentHost(host, s.shortDomain) {
		return 0, nil
	}

	domain, err := s.domains.GetByHostname(ctx, NormalizeHost(host))
	if err != nil {
		return 0, err
	}

	if domain == nil || domain.Status != models.DomainStatusVerified {
		return 0, apierrors.NotFound("short link not found")
	}

	return domain.OrgID, nil
}

// deeplinkURL returns the app link for the visitor's OS, if the link's
// owner-set config covers it (FEATURES.md §3.2). The Android intent URI uses
// the standard Chrome intent syntax, with the fallback URL query-encoded.
func deeplinkURL(cfg *models.DeeplinkConfig, mobileOS string) (string, bool) {
	switch {
	case cfg == nil:
		return "", false
	case cfg.Android != nil && mobileOS == visitor.OSAndroid:
		a := cfg.Android

		return fmt.Sprintf("intent:%s#Intent;package=%s;scheme=%s;S.browser_fallback_url=%s;end;",
			a.Intent, a.Package, a.Scheme, url.QueryEscape(a.FallbackURL)), true
	case cfg.IOS != nil && mobileOS == visitor.OSIOS:
		return cfg.IOS.Intent, true
	default:
		return "", false
	}
}

// matchTargetRules evaluates the link's rules in creation order and returns
// the first rule's URL where ALL present conditions match (FEATURES.md §3.1).
// A rule-listing failure is logged and skipped — resolution never blocks on
// targeting.
func (s *ResolveService) matchTargetRules(ctx *gofr.Context, link *models.Link, mobileOS, city string) (string, bool) {
	rules, err := s.rules.ListByLink(ctx, link.OrgID, link.ID)
	if err != nil {
		ctx.Logger.Errorf("listing target rules for link %d failed (rules skipped): %v", link.ID, err)

		return "", false
	}

	for i := range rules {
		if ruleMatches(&rules[i], mobileOS, city) {
			return rules[i].URL, true
		}
	}

	return "", false
}

// ruleMatches applies a rule's conditions: every present condition must
// match. device_type compares the visitor's mobile OS; location compares the
// GeoIP city. When the city is unknown (GeoIP disabled, or the IP is not in
// the database) a location condition evaluates false — including is_not, so
// an unknown location never triggers a location-conditioned rule.
func ruleMatches(r *models.Rule, mobileOS, city string) bool {
	if r.DeviceType != nil && !conditionMatches(r.DeviceType, mobileOS) {
		return false
	}

	if r.Location != nil && (city == "" || !conditionMatches(r.Location, city)) {
		return false
	}

	return r.DeviceType != nil || r.Location != nil
}

// conditionMatches applies is / is_not over the condition's values,
// case-insensitively: is = any value equals, is_not = no value equals.
func conditionMatches(c *models.RuleCondition, value string) bool {
	anyEqual := false

	for _, v := range c.Values {
		if strings.EqualFold(v, value) {
			anyEqual = true

			break
		}
	}

	if c.Type == models.ConditionIsNot {
		return !anyEqual
	}

	return anyEqual
}

// applyUTMs returns the outgoing URL and the UTM set to record. Links created
// with explicit UTMs already carry them in the destination; everything else
// gets per-click auto-tagging (FEATURES.md §6.2): source from the referrer,
// medium from the device type.
func (s *ResolveService) applyUTMs(link *models.Link, env visitor.Env) (string, utmValues) {
	explicit := utmValues{
		source:   deref(link.UTMSource),
		medium:   deref(link.UTMMedium),
		campaign: deref(link.UTMCampaign),
	}
	if explicit.any() {
		return link.DestinationURL, explicit
	}

	auto := utmValues{
		source: s.utmSourceFor(env),
		medium: "referrer by " + env.DeviceType,
	}

	return appendUTMs(link.DestinationURL, auto), auto
}

// utmSourceFor derives utm_source from the referrer: known platforms map to
// friendly names, other referrers use their host, direct traffic uses the
// deployment's self-source brand.
func (s *ResolveService) utmSourceFor(env visitor.Env) string {
	host := env.ReferrerHost()

	switch {
	case host == "":
		return s.selfSource
	case host == "t.co" || strings.Contains(host, "twitter"):
		return "twitter"
	case strings.Contains(host, "google"):
		return "google"
	case strings.Contains(host, "facebook"):
		return "facebook"
	default:
		return host
	}
}

func deref(s *string) string {
	if s == nil {
		return ""
	}

	return *s
}
