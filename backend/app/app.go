// Package app assembles the ogtr server: configuration, migrations, stores,
// services, handlers, routes and cron jobs. The stock binary (backend
// main.go) calls app.Run() with no options; a deployment may compose the
// same core with its own additions through the functional options
// (ARCHITECTURE.md §8 "Deployment composition") — a limits.Policy, extra
// migrations, extra routes, extra identity providers, extra auth-exempt
// paths. With no options, behavior is identical to the pre-package main.go.
package app

import (
	"context"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"

	"gofr.dev/pkg/gofr"
	"gofr.dev/pkg/gofr/migration"

	"github.com/opengittr/ogtr/backend/auth"
	"github.com/opengittr/ogtr/backend/geo"
	"github.com/opengittr/ogtr/backend/handlers"
	"github.com/opengittr/ogtr/backend/migrations"
	"github.com/opengittr/ogtr/backend/ratelimit"
	"github.com/opengittr/ogtr/backend/scanner"
	"github.com/opengittr/ogtr/backend/services"
	"github.com/opengittr/ogtr/backend/stores"
	"github.com/opengittr/ogtr/backend/usage"
	"github.com/opengittr/ogtr/backend/visitor"
)

// Fixed abuse-defense knobs (deliberately not configurable in v1: the
// values are generous for humans and tight for scripts; LINK_CREATE_RATE is
// the one operators actually need to tune).
const (
	reportRatePerMinute     = 5              // POST /api/v1/report per IP
	guessThrottleLimit      = 60             // unknown-code 404s per IP per minute
	guessThrottleCooldown   = time.Minute    // block length once over the limit
	defaultLinkCreateRate   = 30             // link creates+edits per principal per minute
	defaultBlocklistRefresh = 6 * time.Hour  // BLOCKLIST_REFRESH_INTERVAL default
	defaultRescanInterval   = 24 * time.Hour // RESCAN_INTERVAL default
)

// Run assembles the application and starts serving. It never returns; a
// configuration error refuses to start.
func Run(opts ...Option) {
	app := gofr.New()

	if err := setup(app, newOptions(opts...)); err != nil {
		app.Logger().Fatalf("refusing to start: %v", err)
	}

	app.Run()
}

// setup wires the entire application onto app. Split from Run so tests can
// assemble without serving; a non-nil error means the process must not start.
//
//nolint:funlen // linear assembly of the whole application; splitting would obscure the wiring
func setup(app *gofr.App, o *options) error {
	merged, err := mergeMigrations(migrations.All(), o.migrations)
	if err != nil {
		return err
	}

	app.Migrate(merged)

	signingKey := app.Config.Get("JWT_SIGNING_KEY")
	if signingKey == "" {
		return fmt.Errorf("JWT_SIGNING_KEY is not set (session tokens would be unsigned)")
	}

	tokens := auth.NewTokenIssuer(signingKey,
		durationConfig(app, "ACCESS_TOKEN_TTL", auth.DefaultAccessTTL),
		durationConfig(app, "REFRESH_TOKEN_TTL", auth.DefaultRefreshTTL))

	providers, providerNames, err := buildProviders(app, o.providers)
	if err != nil {
		return err
	}

	shortDomain := app.Config.Get("SHORT_DOMAIN")
	if shortDomain == "" {
		return fmt.Errorf("SHORT_DOMAIN is not set (short URLs cannot be built)")
	}

	shortScheme := app.Config.GetOrDefault("SHORT_SCHEME", "https")
	abuseContact := app.Config.Get("ABUSE_CONTACT")

	locator, cities := loadGeo(app)

	urlScanner := buildScanner(app)
	reserved := services.NewReservedAliases(splitList(app.Config.Get("RESERVED_ALIASES")))

	// In-memory, per-instance rate limiters (ratelimit package doc): link
	// creation/edits per principal, abuse reports per IP, and the resolver's
	// unknown-code guess throttle.
	createLimiter := ratelimit.NewSlidingWindow(intConfig(app, "LINK_CREATE_RATE", defaultLinkCreateRate), time.Minute)
	reportLimiter := ratelimit.NewSlidingWindow(reportRatePerMinute, time.Minute)
	guessThrottle := ratelimit.NewGuessThrottle(guessThrottleLimit, time.Minute, guessThrottleCooldown)

	userStore := stores.NewUserStore()
	orgStore := stores.NewOrgStore()
	memberStore := stores.NewMemberStore()
	inviteStore := stores.NewInviteStore()
	linkStore := stores.NewLinkStore()
	clickStore := stores.NewClickStore()
	ruleStore := stores.NewRuleStore()
	statsStore := stores.NewStatsStore()
	apiKeyStore := stores.NewAPIKeyStore()
	domainStore := stores.NewDomainStore()
	reportStore := stores.NewAbuseReportStore()
	adminStore := stores.NewAdminStore()
	usageStore := usage.NewStore()

	// The deployment's resource policy (ARCHITECTURE.md §8 "Extension seam:
	// LimitsPolicy"): limits.Unlimited{} unless WithPolicy substituted one.
	// An implementation that bounds by current usage is typically constructed
	// with a usage.Reader. Note what is NOT wired to the policy: resolveSvc —
	// resolution and click recording can never be policy-bound (FEATURES.md
	// INV-7).
	policy := o.policy

	authSvc := services.NewAuthService(providers, tokens, userStore, orgStore, memberStore, inviteStore, policy)
	orgSvc := services.NewOrgService(orgStore, memberStore, inviteStore, userStore, policy)
	linkSvc := services.NewLinkService(linkStore, memberStore, domainStore, urlScanner, policy, reserved,
		shortScheme, shortDomain, abuseContact)
	ruleSvc := services.NewRuleService(ruleStore, linkStore)
	resolveSvc := services.NewResolveService(linkStore, clickStore, ruleStore, domainStore, locator,
		guessThrottle, app.Config.GetOrDefault("UTM_SELF_SOURCE", shortDomain), shortDomain, abuseContact)
	reportSvc := services.NewReportService(linkStore, reportStore, reportLimiter)
	rescanSvc := services.NewRescanService(linkStore, urlScanner)
	statsSvc := services.NewStatsService(statsStore, linkStore, ruleStore, policy, usageStore)
	// adminSvc reuses linkStore's SetStatusByID — operator disable/enable
	// shares the re-scan's abuse-status machinery exactly.
	adminSvc := services.NewAdminService(adminStore, linkStore)
	apiKeySvc := services.NewAPIKeyService(apiKeyStore, policy)
	// net.DefaultResolver is the production DNSResolver; tests substitute a
	// mock behind the same interface — the verification code is identical.
	domainSvc := services.NewDomainService(domainStore, memberStore, net.DefaultResolver, policy, shortDomain)

	authH := handlers.NewAuthHandler(authSvc, providerNames,
		app.Config.Get("GOOGLE_CLIENT_ID"), app.Config.Get("MICROSOFT_CLIENT_ID"))
	orgH := handlers.NewOrgHandler(orgSvc, authSvc)
	linkH := handlers.NewLinkHandler(linkSvc, apiKeySvc, createLimiter)
	ruleH := handlers.NewRuleHandler(ruleSvc, cities)
	resolveH := handlers.NewResolveHandler(resolveSvc, apiKeySvc, app.Config.Get("WEBSITE_URL"),
		shortDomain, abuseContact)
	reportH := handlers.NewReportHandler(reportSvc)
	statsH := handlers.NewStatsHandler(statsSvc)
	apiKeyH := handlers.NewAPIKeyHandler(apiKeySvc)
	domainH := handlers.NewDomainHandler(domainSvc)
	adminH := handlers.NewAdminHandler(adminSvc)

	// Instance admin API gate (ARCHITECTURE.md "Instance admin API"):
	// ADMIN_API_TOKEN unset (the default) keeps every /api/internal/* route
	// answering 404 — the feature is dark, like the dev provider.
	adminToken := app.Config.Get("ADMIN_API_TOKEN")
	if adminToken != "" {
		app.Logger().Infof("instance admin API enabled (/api/internal/*, X-Admin-Token auth)")
	}

	app.UseMiddleware(auth.Middleware(tokens, o.exemptPaths...), auth.AdminTokenGate(adminToken),
		visitor.Middleware(), handlers.CacheControl())

	// Session (ARCHITECTURE.md §4). providers + google + microsoft + dev +
	// refresh are auth-exempt. The login routes are always registered; the
	// handler answers 404 for providers not enabled.
	app.GET("/api/v1/auth/providers", authH.Providers)
	app.POST("/api/v1/auth/google", authH.GoogleLogin)
	app.POST("/api/v1/auth/microsoft", authH.MicrosoftLogin)
	app.POST("/api/v1/auth/dev", authH.DevLogin)
	app.POST("/api/v1/auth/refresh", authH.Refresh)
	app.GET("/api/v1/me", authH.Me)
	app.POST("/api/v1/auth/switch-org", authH.SwitchOrg)

	// Orgs & membership (§4). /org singular = "the org in my token".
	app.POST("/api/v1/orgs", orgH.Create)
	app.GET("/api/v1/org", orgH.Get)
	app.PATCH("/api/v1/org", orgH.Update)
	app.GET("/api/v1/org/members", orgH.Members)
	app.DELETE("/api/v1/org/members/{userId}", orgH.RemoveMember)
	app.POST("/api/v1/org/invites", orgH.CreateInvite)
	app.GET("/api/v1/org/invites", orgH.ListInvites)
	app.DELETE("/api/v1/org/invites/{id}", orgH.RevokeInvite)

	// Custom domains (§4 "Custom domains"). Org-scoped via the token;
	// mutations are OWNER-only, members may list. Verification is a DNS TXT
	// ownership check.
	app.POST("/api/v1/org/domains", domainH.Create)
	app.GET("/api/v1/org/domains", domainH.List)
	app.POST("/api/v1/org/domains/{id}/verify", domainH.Verify)
	app.PUT("/api/v1/org/domains/{id}/primary", domainH.SetPrimary)
	app.DELETE("/api/v1/org/domains/{id}", domainH.Delete)

	// Links (§4). Org always inferred from the token, never the path.
	app.POST("/api/v1/links", linkH.Shorten)
	app.GET("/api/v1/links", linkH.List)
	app.GET("/api/v1/links/{id}", linkH.Get)
	app.PATCH("/api/v1/links/{id}", linkH.UpdateDestination)
	app.PUT("/api/v1/links/{id}/alias", linkH.SetAlias)
	app.PUT("/api/v1/links/{id}/deeplink", linkH.SetDeeplink)
	app.GET("/api/v1/links/{id}/qr", linkH.QR)

	// Targeting (§4 "Target rules"). Rules are org-scoped via the token; the
	// cities endpoint powers the rule-builder autocomplete (501 without a
	// city dataset).
	app.POST("/api/v1/links/{id}/rules", ruleH.Create)
	app.GET("/api/v1/links/{id}/rules", ruleH.List)
	app.PUT("/api/v1/rules/{ruleId}", ruleH.Update)
	app.DELETE("/api/v1/rules/{ruleId}", ruleH.Delete)
	app.GET("/api/v1/cities", ruleH.Cities)

	// Analytics (§4 "Analytics"). The per-link report shares link-detail
	// visibility; the org-level endpoints are token-org-scoped.
	app.GET("/api/v1/links/{id}/stats", statsH.LinkReport)
	app.GET("/api/v1/stats/unique-clicks", statsH.UniqueClicks)
	app.GET("/api/v1/stats/tags", statsH.Tags)
	app.GET("/api/v1/stats/utm", statsH.UTM)

	// Developer API keys (§4 "API keys"). JWT-managed, org-scoped; the
	// created key's plaintext is returned exactly once. DELETE disables —
	// keys are never hard-deleted (link attribution history).
	app.POST("/api/v1/api-keys", apiKeyH.Create)
	app.GET("/api/v1/api-keys", apiKeyH.List)
	app.DELETE("/api/v1/api-keys/{id}", apiKeyH.Disable)

	// Public abuse reporting (auth-exempt, per-IP rate-limited): anyone who
	// received a bad short link can flag it without an account.
	app.POST("/api/v1/report", reportH.Create)

	// Instance admin API (operator surface; deliberately cross-org — the
	// sanctioned INV-6 exception). Guarded by AdminTokenGate above, never by
	// JWT; all six groups 404 until ADMIN_API_TOKEN is set.
	app.GET("/api/internal/users", adminH.Users)
	app.GET("/api/internal/orgs", adminH.Orgs)
	app.GET("/api/internal/reports", adminH.Reports)
	app.GET("/api/internal/links/{id}", adminH.Link)
	app.POST("/api/internal/links/{id}/disable", adminH.DisableLink)
	app.POST("/api/internal/links/{id}/enable", adminH.EnableLink)
	app.GET("/api/internal/stats/daily", adminH.DailyStats)

	// Public resolution (§4). /api/v1/resolve is auth-exempt; the root
	// redirect pattern's charset excludes dots, so gofr's own single-segment
	// routes (/favicon.ico — registered at Run(), after this) and the dotted
	// /.well-known/* paths can never be shadowed by it.
	//
	// GET /{code}+ (trailing plus — outside the redirect charset, so the two
	// routes can never collide) is the no-click link preview page.
	app.GET("/api/v1/resolve", resolveH.Resolve)
	app.GET("/{code:[a-zA-Z0-9_-]+}", resolveH.Redirect)
	app.GET("/{code:[a-zA-Z0-9_-]+}+", resolveH.Preview)

	// Bare-domain bounce: GET / (no code) 302s to WEBSITE_URL when that
	// config is set; without it the root stays a 404, exactly as before.
	// Public route — the auth middleware only guards /api/*.
	app.GET("/", resolveH.Root)

	// Periodic destination re-scan (gofr cron): links clicked in the last 7
	// days get their destinations re-checked; flagged ones flip to
	// DISABLED_ABUSE and answer 410. Interval RESCAN_INTERVAL (default 24h).
	rescanEvery := durationConfig(app, "RESCAN_INTERVAL", defaultRescanInterval)
	app.AddCronJob(cronSpec(rescanEvery), "destination-rescan", func(ctx *gofr.Context) {
		rescanSvc.Run(ctx)
	})

	// Deployment-registered routes come last, after every core route.
	if o.routes != nil {
		o.routes(app, &Services{Usage: usageStore, Members: memberStore})
	}

	return nil
}

// mergeMigrations merges deployment migrations after the core's; a timestamp
// collision is a wiring error and refuses to start.
func mergeMigrations(core, extra map[int64]migration.Migrate) (map[int64]migration.Migrate, error) {
	if len(extra) == 0 {
		return core, nil
	}

	merged := make(map[int64]migration.Migrate, len(core)+len(extra))

	for key, m := range core {
		merged[key] = m
	}

	for key, m := range extra {
		if _, exists := merged[key]; exists {
			return nil, fmt.Errorf("migration %d is defined by both the core and the deployment", key)
		}

		merged[key] = m
	}

	return merged, nil
}

// buildProviders assembles the identity-provider map: AUTH_PROVIDERS decides
// which built-in sign-in methods exist (ARCHITECTURE.md §5; unknown values
// refuse to start; "dev" trusts any submitted identity and exists only so a
// fresh install can be evaluated without Google setup), and WithProviders
// entries are merged on top (added names become login-enabled; a name
// collision replaces the built-in).
func buildProviders(app *gofr.App, extra map[string]auth.IdentityProvider,
) (map[string]auth.IdentityProvider, []string, error) {
	providerNames, err := auth.ParseProviders(app.Config.Get("AUTH_PROVIDERS"))
	if err != nil {
		return nil, nil, err
	}

	providers := make(map[string]auth.IdentityProvider, len(providerNames)+len(extra))

	for _, name := range providerNames {
		switch name {
		case auth.ProviderGoogle:
			providers[name] = auth.NewGoogleProvider(
				app.Config.Get("GOOGLE_CLIENT_ID"),
				app.Config.GetOrDefault("GOOGLE_JWKS_URL", auth.DefaultGoogleJWKSURL))
		case auth.ProviderMicrosoft:
			providers[name] = auth.NewMicrosoftProvider(
				app.Config.Get("MICROSOFT_CLIENT_ID"),
				app.Config.GetOrDefault("MICROSOFT_JWKS_URL", auth.DefaultMicrosoftJWKSURL))
		case auth.ProviderDev:
			providers[name] = auth.NewDevProvider()

			app.Logger().Warn("dev login is enabled (AUTH_PROVIDERS includes \"dev\") — " +
				"anyone can sign in as any identity; do not use in production")
		}
	}

	extraNames := make([]string, 0, len(extra))

	for name, provider := range extra {
		if _, exists := providers[name]; !exists {
			extraNames = append(extraNames, name)
		}

		providers[name] = provider
	}

	sort.Strings(extraNames) // map order is random; keep the enabled list deterministic

	return providers, append(providerNames, extraNames...), nil
}

// buildScanner assembles the URL-scanning pipeline (ARCHITECTURE.md
// "URL scanning & abuse defenses"): syntactic guards always on, feed
// blocklists when BLOCKLIST_FEED_URLS is set (initial load in the
// background — feeds never block startup — plus cron refresh), Google Web
// Risk when WEBRISK_API_KEY is set (fail-open).
func buildScanner(app *gofr.App) *scanner.Pipeline {
	log := app.Logger()

	layers := []scanner.Scanner{
		scanner.NewSyntactic(splitList(app.Config.Get("SHORTENER_DOMAINS"))),
	}

	if feedURLs := splitList(app.Config.Get("BLOCKLIST_FEED_URLS")); len(feedURLs) > 0 {
		feeds := scanner.NewFeedList(feedURLs, nil, log)

		go feeds.Refresh(context.Background()) // startup load, off the boot path

		refreshEvery := durationConfig(app, "BLOCKLIST_REFRESH_INTERVAL", defaultBlocklistRefresh)
		app.AddCronJob(cronSpec(refreshEvery), "blocklist-refresh", func(ctx *gofr.Context) {
			feeds.Refresh(ctx)
		})

		layers = append(layers, feeds)

		log.Infof("blocklist feeds enabled (%d feeds, refresh %s)", len(feedURLs), refreshEvery)
	}

	if apiKey := app.Config.Get("WEBRISK_API_KEY"); apiKey != "" {
		layers = append(layers, scanner.NewWebRisk(apiKey, "", nil, log))

		log.Infof("web risk destination scanning enabled")
	}

	return scanner.NewPipeline(log, layers...)
}

// cronSpec maps an interval onto a gofr cron schedule (gofr crons support
// an optional seconds field). Intervals of 24h and above run once daily —
// interval semantics degrade to "daily" past that point.
func cronSpec(d time.Duration) string {
	switch {
	case d < time.Minute:
		return fmt.Sprintf("*/%d * * * * *", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("*/%d * * * *", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("0 */%d * * *", int(d.Hours()))
	default:
		return "0 4 * * *"
	}
}

// splitList parses a comma-separated config value into trimmed non-empty
// items.
func splitList(raw string) []string {
	var items []string

	for item := range strings.SplitSeq(raw, ",") {
		if item = strings.TrimSpace(item); item != "" {
			items = append(items, item)
		}
	}

	return items
}

// intConfig reads a positive integer config, falling back (loudly) to the
// default.
func intConfig(app *gofr.App, key string, fallback int) int {
	raw := app.Config.Get(key)
	if raw == "" {
		return fallback
	}

	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		app.Logger().Errorf("invalid %s %q, using default %d", key, raw, fallback)

		return fallback
	}

	return n
}

// durationConfig reads a positive Go-duration config, falling back (loudly)
// to the default.
func durationConfig(app *gofr.App, key string, fallback time.Duration) time.Duration {
	raw := app.Config.Get(key)
	if raw == "" {
		return fallback
	}

	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		app.Logger().Errorf("invalid %s %q, using default %s", key, raw, fallback)

		return fallback
	}

	return d
}

// loadGeo initializes the optional GeoIP datasets (ARCHITECTURE.md §6). The
// .mmdb is opened ONCE and the handle shared for the process lifetime; the
// city CSV is read once into memory. An unset path disables that dataset; a
// set-but-unloadable path logs an error and degrades the same way (clicks
// without city, location rules never match, /api/v1/cities 501).
func loadGeo(app *gofr.App) (locator *geo.Locator, cities *geo.Cities) {
	locator, cities = geo.DisabledLocator(), geo.DisabledCities()

	if path := app.Config.Get("GEOIP_DB_PATH"); path != "" {
		opened, err := geo.OpenLocator(path)
		if err != nil {
			app.Logger().Errorf("GEOIP_DB_PATH %q could not be opened (GeoIP disabled): %v", path, err)
		} else {
			locator = opened

			app.Logger().Infof("GeoIP city database loaded from %s", path)
		}
	}

	if path := app.Config.Get("GEOIP_CITIES_CSV"); path != "" {
		loaded, err := geo.LoadCities(path)
		if err != nil {
			app.Logger().Errorf("GEOIP_CITIES_CSV %q could not be loaded (city autocomplete disabled): %v", path, err)
		} else {
			cities = loaded

			app.Logger().Infof("city autocomplete dataset loaded from %s", path)
		}
	}

	return locator, cities
}
