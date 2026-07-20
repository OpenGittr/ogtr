package services

import "strings"

// Reserved aliases — words that can never become short codes. Short codes
// share the root path of the short domain with service routes and with the
// URLs people *expect* a product domain to have, so an attacker who claims
// one gets a perfectly plausible-looking phishing or squatting URL on the
// deployment's own domain.
//
// Three curated categories (rationale in docs/DEPLOYMENT.md):
//
//   - auth-shaped: words that make a credential-stealing page look native
//     ("https://links.example.com/login" — auth phishing on your own domain).
//   - functional/infra: words that shadow (or will shadow) real service
//     routes and infra paths — these apply on EVERY domain, because routing
//     is path-based regardless of the request host.
//   - legal/brand: pages a company is expected to serve at the root of its
//     domain (terms, pricing, docs, abuse …) — squatting them misrepresents
//     the deployment itself.
//
// Scoping: on the shared SHORT_DOMAIN all three categories (plus
// RESERVED_ALIASES config additions) are reserved. On an org with a
// VERIFIED custom domain only the functional/infra category and the config
// additions apply — the org owns its own namespace, so "pricing" on
// links.their-brand.com is theirs to use.
var (
	// Auth-shaped: sign-in/account flows an attacker would imitate.
	reservedAuthShaped = []string{
		"signin", "signup", "sign-in", "sign-up", "login", "logout", "register",
		"password", "passwords", "reset", "reset-password", "forgot-password",
		"verify", "verification", "confirm", "activate", "auth", "oauth",
		"oauth2", "sso", "saml", "openid", "token", "session", "sessions",
		"account", "accounts", "profile", "settings", "billing", "subscribe",
		"unsubscribe", "invite", "invites", "member", "members", "admin",
		"administrator", "root", "dashboard", "console",
	}

	// Functional/infra: service routes, well-known paths and infra names.
	// Applies on every domain, including verified custom domains.
	reservedFunctional = []string{
		"api", "app", "www", "web", "mail", "email", "smtp", "ftp", "static",
		"assets", "asset", "cdn", "img", "images", "image", "css", "js",
		"fonts", "media", "files", "file", "download", "downloads", "upload",
		"uploads", "metrics", "health", "healthz", "alive", "ready", "ping",
		"debug", "internal", "system", "config", "webhook", "webhooks",
		"callback", "callbacks", "graphql", "rpc", "grpc", "swagger",
		"openapi", "favicon.ico", "robots.txt", "sitemap.xml", ".well-known",
		"null", "undefined", "test", "localhost",
	}

	// Legal/brand: pages a product domain is expected to serve itself.
	reservedLegalBrand = []string{
		"terms", "tos", "privacy", "legal", "policy", "policies", "cookies",
		"gdpr", "dmca", "copyright", "license", "imprint", "pricing", "plans",
		"features", "compare", "self-host", "selfhost", "enterprise", "docs",
		"documentation", "developers", "blog", "news", "changelog", "status",
		"abuse", "report", "security", "trust", "support", "help", "faq",
		"contact", "about", "team", "careers", "jobs", "press", "partners",
	}
)

// ReservedAliases answers "may this word become a short code?" with the
// category scoping above. Immutable after construction — safe for
// concurrent use.
type ReservedAliases struct {
	full       map[string]struct{} // all three categories + config additions
	functional map[string]struct{} // functional/infra + config additions
}

// NewReservedAliases builds the checker. extra comes from the
// RESERVED_ALIASES config (deployment-specific additions); those are
// operator-mandated and therefore reserved in BOTH scopes.
func NewReservedAliases(extra []string) *ReservedAliases {
	r := &ReservedAliases{
		full:       map[string]struct{}{},
		functional: map[string]struct{}{},
	}

	for _, list := range [][]string{reservedAuthShaped, reservedFunctional, reservedLegalBrand} {
		for _, word := range list {
			r.full[word] = struct{}{}
		}
	}

	for _, word := range reservedFunctional {
		r.functional[word] = struct{}{}
	}

	for _, word := range extra {
		if word = strings.ToLower(strings.TrimSpace(word)); word != "" {
			r.full[word] = struct{}{}
			r.functional[word] = struct{}{}
		}
	}

	return r
}

// IsReserved reports whether the word (case-insensitive) is reserved in the
// given scope: customDomainScope=true applies only the functional/infra
// category (plus config additions) — the relaxed rule for orgs with a
// VERIFIED custom domain.
func (r *ReservedAliases) IsReserved(word string, customDomainScope bool) bool {
	lower := strings.ToLower(word)

	if customDomainScope {
		_, ok := r.functional[lower]

		return ok
	}

	_, ok := r.full[lower]

	return ok
}
