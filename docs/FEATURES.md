# ogtr — Feature Specification

This document is the authoritative feature list for `ogtr`, the open-source,
self-deployable link shortener under the opengittr org. It describes what the product does;
`docs/ARCHITECTURE.md` describes how it is built.

Sections:

1. [Link management](#1-link-management)
2. [Redirection & link resolution](#2-redirection--link-resolution)
3. [Targeting & deep links](#3-targeting--deep-links)
4. [Click tracking & visitor detection](#4-click-tracking--visitor-detection)
5. [Analytics & reporting](#5-analytics--reporting)
6. [UTM campaign management](#6-utm-campaign-management)
7. [Accounts, auth & multi-tenancy](#7-accounts-auth--multi-tenancy)
8. [Developer API keys](#8-developer-api-keys)
9. [Operational features](#9-operational-features)
10. [Abuse protection](#10-abuse-protection)
11. [Design invariants](#11-design-invariants)
12. [Roadmap](#12-roadmap)

---

## 1. Link management

### 1.1 Shorten a URL

- Accepts a long destination URL and returns a short link on the deployment's domain
  (`SHORT_DOMAIN` config).
- **Scheme normalization** — input without `http://`/`https://` is auto-prefixed with
  `https://`; malformed URLs are rejected with a clear validation error.
- **Already-short detection** — submitting a URL that is already a short link on this host
  returns it unchanged.
- **Per-tenant deduplication** — shortening the same destination URL twice within a tenant
  returns the existing short link instead of creating a duplicate. Dedupe (and already-short
  detection) only matches links the caller can see — another user's PRIVATE link never comes
  back from a dedupe hit.
- **Short code generation** — compact random base62 code, unique per link across generated
  codes *and* custom aliases (see INV-5: links are not enumerable).
- **QR code** — every short link has a QR code (PNG), generated on demand from the short URL
  and retrievable at any time.
- **Link visibility type** — `PUBLIC` (default) or `PRIVATE`. Creating a private link requires
  an authenticated user; private links are only listed to their owner (others get 404, so even
  existence is not revealed).
- **UTM parameters at creation** — optional `source`, `medium`, `campaign` fields are appended
  to the destination URL as `utm_source`, `utm_medium`, `utm_campaign` query params.
- **Ownership attribution** — each link records its creating user, tenant, creation timestamp,
  and (for API-created links) the developer key that created it.

### 1.2 Custom short codes (branded aliases)

- A user can replace a link's generated code with a custom alias.
- Alias format: 3–50 characters from `[a-zA-Z0-9_-]`.
- Uniqueness enforced across the deployment; conflict returns HTTP 409.
- A reserved-word blacklist (`api`, `login`, `metrics`, …) keeps aliases from shadowing
  service routes.
- The alias becomes the canonical short URL for the link (dashboard, QR, analytics all follow);
  the previous generated code stops resolving.

### 1.3 Link listing / dashboard

- Paginated list (10 per page) of the authenticated user's links within their tenant.
- Per link: ID, short/destination URL, created-at, last-visited timestamp, QR code.

### 1.4 Per-link counters

- Every link tracks a total visit count and a last-visit timestamp, updated on each resolution.

### 1.5 Destination editing

- A link's destination can be repointed after creation (`PATCH /api/v1/links/{id}`) — printed
  QR codes and published short links stay useful when the target moves.
- Accepts a new URL plus optional UTM source/medium/campaign; validation is identical to
  creation (scheme normalization, 422 on malformed, own short domain rejected). Deduplication
  stays creation-time-only — an edit never merges links.
- **Permission**: the link's creator or an org OWNER (role checked against the DB). Any other
  member gets 403; cross-org IDs and other users' PRIVATE links stay 404. API-key-created
  links (no creator) are editable by OWNERs only.
- **Audit trail**: every applied edit writes a `link_edits` row (org, link, user, old URL, new
  URL, timestamp). Clicks keep accruing to the link across edits; annotating analytics charts
  with edit markers is future work enabled by this table.

### 1.6 Custom domains (per-org branded short domains)

- An org can bring its own hostname (e.g. `links.their-brand.com`) for its short links.
- **Registration** (org OWNER): submit a bare hostname — validated as a real DNS name
  (lowercase, punycode-normalized; no scheme/path/port/IP literals; at least two labels). The
  deployment's own short domain, or any subdomain of it, is rejected. Hostnames are globally
  unique across the deployment — one org owns a hostname (conflict → HTTP 409).
- **Ownership verification**: the org publishes a DNS TXT record at
  `_ogtr-verify.<hostname>` whose value is a server-generated random token, then triggers a
  verification check. Success flips the domain to VERIFIED; until DNS proves ownership the
  check answers 409 with a human-readable reason (record missing, not yet propagated, or
  wrong value).
- **Serving**: requests arriving on a VERIFIED custom domain resolve **only that org's
  links** — another org's code is a 404, as is any unverified or unknown hostname, and the
  bare `/` of a custom domain. The deployment's own short domain keeps resolving every link
  (the global code namespace is unchanged); a custom domain adds a scoped entry point.
- **Primary domain & display**: one domain per org can be marked primary (VERIFIED only).
  Short URLs everywhere — create responses, link lists/detail, the success card and QR
  codes — are then built as `https://<primary-domain>/<code>`. Deleting or unsetting it
  reverts the display to the deployment domain; links keep working throughout.
- **Roles**: OWNER for add/verify/set-primary/delete; every member can view the list and
  instructions.
- TLS for custom domains is a deployment/ingress concern, not application logic
  (DEPLOYMENT.md).

---

## 2. Redirection & link resolution

### 2.1 Resolve a short link

- `GET /{code}` serves a **real HTTP redirect** (302 with `Cache-Control: no-store`) so short
  links work in any browser, mail client, or QR scan with no frontend involved (INV-1).
  Resolution never requires login. Unknown codes return 404.
- Resolution is **host-aware** (§1.6): on the deployment's short domain every link resolves;
  on a VERIFIED custom domain only the owning org's links resolve; any other host is 404.
- A JSON resolution API (`GET /api/v1/resolve`) exists for programmatic use, with an optional
  campaign tag attached to the click.
- Every resolution records a click (§4) — via either endpoint (INV-4).
- The final destination is computed through the pipeline: deep-link matching (§3.2) →
  target-rule matching (§3.1) → UTM auto-tagging (§6.2) → destination URL.

---

## 3. Targeting & deep links

### 3.1 Target rules (conditional redirects)

Per-link routing rules that send different visitors to different destinations.

- Each rule has a **target name**, a **destination URL**, and conditions on:
  - **Device / mobile OS** — e.g. iOS, Android.
  - **Location (city)** — visitor city resolved from IP via a GeoIP database (MaxMind
    GeoLite2-City).
- Condition operators: `is` and `is_not`, each with a list of values (case-insensitive match).
- A rule matches only when **all** of its conditions match; the first matching rule's URL is
  used (rules evaluate in creation order). No match → default destination.
- Whether a click was served by a target rule is recorded per click (`target_matched`) and
  reported in analytics (§5.1).
- Full CRUD: add one or more rules to a link, list a link's rules, edit a rule, delete a rule —
  all tenant-scoped.
- **City-name autocomplete** for the rule-builder UI, backed by the GeoLite2 city-names
  dataset.

### 3.2 Mobile deep links

- A link can carry platform-specific app metadata:
  - **Android** — intent URI built from `intent`, `package`, `scheme`, and
    `S.browser_fallback_url` (fallback when the app isn't installed).
  - **iOS** — app intent/universal link.
- On resolution, if the visitor's OS matches configured deep-link metadata, the app deep link is
  returned/served instead of the web URL.
- Deep-link metadata is **owner-configured only** — set at creation or via an authenticated
  update; resolution never mutates the link (INV-3).
- Deep-link usage is recorded per click (`is_deeplink`) and reported in analytics (§5.1).

---

## 4. Click tracking & visitor detection

Every resolution records one click event (INV-4) with:

| Field | Detail |
|---|---|
| Link + custom-code reference | which link and alias was clicked |
| Timestamp | click time |
| UTM source / medium / campaign | explicit or auto-tagged (§6.2) |
| Device type | Mobile / Tablet / Desktop (User-Agent) |
| Mobile OS | iOS / Android / Windows / Other / NA |
| Browser | Chrome, Safari, Firefox, Opera, IE, Other |
| Referrer | `Referer` header, `Direct` when absent |
| IP address | first valid public IP from forwarding headers, else remote addr |
| Geo city / region / country | from GeoIP lookup (city also feeds target rules) |
| Browser grade | UA quality grade |
| `is_deeplink` | whether an app deep link was served |
| `target_matched` | whether a target rule matched |
| Custom tag ID | optional campaign tag attached to the click (§5.3) |
| Tenant | owning tenant |

Click recording never blocks the redirect: GeoIP/parse failures degrade fields to empty, and
write errors are logged and swallowed — the visitor is redirected regardless.

---

## 5. Analytics & reporting

### 5.1 Per-link analysis report

Date-ranged report for one link (default window: last month; validates `start <= end`):

- **Clicks per day** (time series).
- **Time-series breakdowns** (per day): by browser, device type, referrer, mobile OS,
  and location.
- **Aggregate breakdowns** (whole range): by browser, device type, referrer, mobile OS,
  and location.
- **Location breakdown** — reported at three levels (country / region / city), both
  whole-range totals and per-day. Clicks without geo data (GeoIP not configured,
  unresolvable IP) bucket as `Unknown`.
- **Total clicks** in range.
- **Deep-link stats** — count of clicks served an app deep link; mobile app-open count.
- **Target-rule effectiveness** — total clicks vs. clicks where a target rule matched (only when
  the link has rules).
- **Detailed click list** — individual click records (ID, link, custom tag, timestamp).

### 5.2 Unique clicks

- Given a set of link IDs, returns the count of distinct campaign tags that clicked,
  tenant-scoped.

### 5.3 Campaign tag listing

- Lists all distinct custom tag IDs seen in the tenant's click data.

---

## 6. UTM campaign management

### 6.1 Explicit UTM tagging

- UTM `source` / `medium` / `campaign` provided at link creation are appended to the destination
  URL as proper query parameters (URL-encoded, preserving existing query and fragment).

### 6.2 UTM auto-tagging

When a link has no explicit UTM parameters, they are inferred per click:

- `utm_source` — derived from the referrer: known platforms mapped to friendly names
  (google, twitter/t.co, facebook), otherwise the referrer host; for direct traffic,
  `UTM_SELF_SOURCE` when set, else the deployment's `SHORT_DOMAIN`.
- `utm_medium` — derived from the visitor's device type (e.g. `referrer by Mobile`).
- Auto-tagged values are recorded in click stats and appended to the outbound destination URL.

### 6.3 UTM analysis

- For the authenticated user's links: click counts grouped by `utm_source`, `utm_medium`, and
  `utm_campaign`, each broken down per destination URL. Returned as three parallel analyses
  (source / medium / campaign). Scoped to links the viewer can see — another user's PRIVATE
  link never leaks through aggregate rows.

---

## 7. Accounts, auth & multi-tenancy

### 7.1 Multi-tenancy

- Every entity (user, link, alias, rule, click, developer key) carries a tenant (org) ID; all
  queries and all analytics are tenant-scoped (INV-6). One deployment serves many
  organizations.
- Tenant identity comes from the authenticated token's claims, never from client-supplied
  fields.
- Orgs are first-class and self-serve: creation, email invites, roles (OWNER/MEMBER), org
  switching, and optional per-org email-domain auto-join. Multi-tenant always — even a solo
  self-hosted deployment creates an org during setup; there is no separate single-tenant mode.

### 7.2 Authentication

- Bearer-token (JWT) auth middleware on all endpoints except: login, refresh, link resolution,
  health checks, metrics, and OpenAPI/swagger paths.
- OAuth-based login (Google) through a pluggable auth provider boundary
  (ARCHITECTURE.md §5) — additional identity providers slot in behind the same interface.
- Token refresh flow that enriches tokens with app-level claims (user ID, name).
- **Just-in-time user provisioning** — first login auto-creates the user record from token
  claims (name, email), converts any pending invites, and applies org auto-join by email
  domain.
- **Dev sign-in provider** — an explicit opt-in provider (`AUTH_PROVIDERS=dev`) that accepts
  any name+email with no credential check, so a fresh install can be evaluated without
  creating a Google OAuth client. Disabled by default (its endpoint 404s), boot-time WARN when
  enabled, never for production.

### 7.3 User model

- Name, email (unique), phone, status (`ENABLED`/…), join timestamp, signup medium, tenant
  memberships.

---

## 8. Developer API keys

- Per-tenant named API keys with `ENABLED`/`DISABLED` status; only a hash of the key is
  stored, the plaintext is shown exactly once at creation.
- API-key authentication (`X-API-Key`) as an alternative to user JWT for the link-creation and
  resolution APIs.
- Links created via API are attributed to the developer key (`api_key_id` on the link) and
  shown as created "via API" in the dashboard.
- Key management CRUD: create/list/disable. Rotation = create a new key, then disable the old —
  there is no in-place rotate endpoint; disabled keys stay listed for attribution.

---

## 9. Operational features

- **MySQL** primary datastore; schema managed by framework migrations, run at boot.
- **Health checks** at well-known paths (used by Kubernetes probes), exposed without auth.
- **Prometheus metrics** on a dedicated metrics port.
- **OpenAPI/Swagger** documentation paths exposed without auth.
- **Structured logging** with configurable level.
- **Container deployment** — Docker images, Kubernetes manifests, CI with test + build gates.
- **GeoIP database** — GeoLite2-City for click geo-resolution and location targeting; loaded
  once at startup from a configurable path and held for the process lifetime. Optional — the
  deployment works (minus location features) without it. The data is MaxMind-licensed and
  never shipped in the repo or images; operators mount it with their own license key.

---

## 10. Abuse protection

Built-in defenses against the ways link shorteners get weaponized (phishing
behind an innocent-looking domain, malware distribution, internal-network
probing). All of it works out of the box; external data sources are opt-in
config (DEPLOYMENT.md §7).

### 10.1 Destination scanning

Every destination URL is scanned **at link creation and at destination
edit** (both the signed-in and the API-key paths) through a layered
pipeline:

- **Syntactic guards** (always on, zero config): rejects non-http(s)
  schemes, `user@host` deception URLs, IP-literal hosts (public *and*
  private/loopback/link-local — a shortened `http://10.0.0.5/` turns every
  visitor into an internal-network probe), `localhost`-style hosts,
  mixed-script (homograph) hostname labels, and destinations that are
  themselves URL shorteners (chaining hides the real target from every
  later check; built-in list + `SHORTENER_DOMAINS` additions).
- **Blocklist feeds** (`BLOCKLIST_FEED_URLS`): plaintext host/URL feeds
  (URLhaus, OpenPhish, CoinBlocker …), loaded in the background at startup
  and refreshed on an interval. A feed outage keeps the last-good data and
  never blocks startup.
- **Google Web Risk** (`WEBRISK_API_KEY`): optional external lookup with a
  hard 2s timeout that **fails open** — the local layers are the floor.

A flagged destination is refused with **422** and a deliberately coarse
message ("flagged by security checks", plus the `ABUSE_CONTACT` appeal line
when configured). Which list or rule matched is never revealed — that would
be a testing oracle for attackers.

### 10.2 Periodic re-scan & link disabling

Destinations that were clean at creation can turn malicious later, so links
**clicked in the last 7 days** are re-scanned on an interval
(`RESCAN_INTERVAL`, default 24h; batched and bounded per run). A flagged
link gets **status `DISABLED_ABUSE`**: `GET /{code}` serves a 410 HTML
warning page ("This link has been disabled" + coarse reason + abuse
contact) instead of redirecting, and the JSON resolve answers 410. The
link's row, code and analytics survive. Re-enabling after a successful
appeal is a deliberate **operator DB action** (DEPLOYMENT.md §7) — nothing
re-enables automatically.

### 10.3 Link preview & public abuse reporting

- **Preview** — appending `+` to any short URL (`GET /{code}+`) shows an
  org-neutral HTML page with the destination **as text** (no styled
  call-to-action), a plain proceed link and a report form. **No click is
  recorded.** Works on custom domains with the same org scoping as
  resolution.
- **Reporting** — `POST /api/v1/report` `{code, reason ≤140 chars,
  reporter_contact?}` is public (recipients of a bad link have no account
  here), rate-limited per IP, and files a row in `abuse_reports` for
  operator triage. Reports never auto-disable a link — the report endpoint
  must not itself become a takedown weapon. Unknown codes answer an honest
  404.

### 10.4 Rate limits

In-memory, per-instance sliding windows (per-replica in Kubernetes; resets
on restart — accepted v1 trade-off, a shared store is future work):

- Link creation + destination edits: `LINK_CREATE_RATE`/min (default 30)
  per authenticated user or API key.
- Abuse reports: 5/min per IP.
- Resolution guess-throttle: more than 60 unknown-code 404s per minute from
  one IP → 429 for a one-minute cooldown (anti-enumeration; successful
  resolutions never count).

### 10.5 Reserved words

A curated, categorized blacklist keeps short codes from impersonating pages
a product domain is expected to have: **auth-shaped** (`login`, `signin`,
`password`, …), **functional/infra** (`api`, `assets`, `webhook`, `www`,
…) and **legal/brand** (`terms`, `pricing`, `abuse`, `status`, …), plus
`RESERVED_ALIASES` config additions. The full set applies on the shared
short domain; an org with a **verified custom domain** is only bound by the
functional/infra category (and config additions) — it owns its own
namespace. The check applies to custom aliases *and* to generated codes
(a random draw that spells a reserved word is redrawn).

---

## 11. Design invariants

Guarantees the implementation must uphold. Each is covered by regression tests; code comments
reference these by their stable identifiers (INV-1 … INV-6).

- **INV-1 — Real HTTP redirects.** `GET /{code}` answers with an actual 302 (and
  `Cache-Control: no-store`); resolution never depends on a frontend to perform the redirect.
- **INV-2 — Allowlisted click columns.** Click-stat writes use a fixed, allowlisted column
  set; nothing derived from visitor input (UTM names, URLs, headers) is ever interpolated
  into SQL.
- **INV-3 — Owner-only deep-link config.** Deep-link metadata is written only by
  authenticated owner actions; the unauthenticated resolution path never mutates a link.
- **INV-4 — Every resolution records a click.** All links — generated code or custom alias,
  redirect or JSON resolution — get a click row.
- **INV-5 — Non-enumerable codes, one namespace.** Short codes are random base62, never
  derived from row IDs, and share a single uniqueness namespace with custom aliases — links
  (including PRIVATE ones) cannot be enumerated and codes can never collide with aliases.
- **INV-6 — Org-scoped everything.** Every query filters by the org derived from the auth
  context — lookups, mutations, and analytics alike. No exceptions.

---

## 12. Roadmap

Tracked as future work, separate from the shipped feature set above:

- Link deletion/archival.
- Link expiry (time- or click-count-based) and scheduled activation.
- Password-protected links.
- Bulk shortening (CSV / API batch).
- Bot filtering in analytics (recording bot UAs but excluding them from reports).
- Webhooks or event export for clicks.
- Shared-store (e.g. Redis) rate limiting for exact global limits across replicas
  (v1 limits are per-instance, §10.4).
- Admin UI: tenant management, usage overview, abuse-report triage.
- Automated per-domain TLS for custom domains in hosted deployments (the app-level custom
  domain feature shipped in §1.6; certificate automation remains an operator concern).
