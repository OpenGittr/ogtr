# ogtr — Architecture

The feature spec is `docs/FEATURES.md`; this document defines *how* those features are built,
including the settled design decisions and per-phase implementation notes.

## 1. Components

```
backend            Go + Gofr + MySQL. ONE module, TWO service entrypoints:
  backend/server   the public product:
                     GET /{code}        → redirect + click recording (public)
                     /api/v1/*          → management API (JWT or API-key auth)
  backend/admin    the instance-admin service (§4 "Instance admin service"):
                     /api/internal/*    → operator API, X-Admin-Token gate.
                   Deployed as `ogtr-internal`, CLUSTER-INTERNAL ONLY — never
                   on any public ingress. (The directory is `admin`, not
                   `internal`, only because `internal/` is Go's reserved
                   import-restriction directory name; the deployment/service
                   keeps the conventional internal-service name.)
frontend           React + Tailwind SPA (dashboard). CSR, served by gostatic.
k8s/               All deployment manifests.
```

Deployment domains (all configurable):

- **SHORT_DOMAIN** (e.g. `links.example.com`) → `backend`. Serves redirects at root and
  the API under `/api/v1`.
- **APP_DOMAIN** (e.g. `app.example.com`) → `frontend`.

There is no marketing-site component in this repo. A deployment may serve its own
marketing site wherever it likes — point the optional `WEBSITE_URL` config at it (bare
`GET /` on the short domain then 302s there) or route to it at the ingress.

One deployment-wide short domain, plus optional **per-org custom domains** (§4 "Custom
domains"): an org can bring `links.their-brand.com`, prove ownership via a DNS TXT record, and
serve/display its short links on it.

**Settled decisions** (do not relitigate):

- **Tenancy in URLs**: tenant/org is always inferred from the authenticated user's token, never
  from the URL path. No `/orgs/{orgId}/...` path scoping.
- **Multi-tenant always**: orgs are first-class from the first request. Even a solo self-hosted
  deployment creates an org during setup. One code path, no "single-tenant mode".
- **Backend shape**: one Go module rooted at `backend/` (shared `stores`/`services`/
  `handlers`/`models`/... packages) with two sibling `package main` entrypoints:
  `backend/server` (the public product) and `backend/admin` (the cluster-internal
  instance-admin service). Redirects served at root (`GET /{code}`), management API under
  `/api/v1/*`. Custom aliases are checked against a reserved-word blacklist (`api`,
  `.well-known`, `metrics`, `login`, etc.) so they can never shadow service routes.
- **Auth**: Google and Microsoft login (OIDC) behind a pluggable auth provider boundary (§5)
  so other identity providers can be added without touching the rest of the login flow.
- **Open core**: billing/plan logic lives outside this repo; never add commercial features,
  license checks, or plan logic here. The only such extension point is the neutral
  `limits.Policy` seam (§8 "Extension seam: LimitsPolicy"), default unlimited.
- **Stack**: Go + Gofr + MySQL for the backend; React + Tailwind (CSR, static-served) for both
  frontends; Docker on Kubernetes for deployment (manifests in `k8s/`). No SSR, no nginx.

## 2. Data model (MySQL)

All tables carry `org_id` except `users` and `orgs` themselves. Every query filters by `org_id`
derived from the auth context (FEATURES.md INV-6). The single sanctioned exception is the
instance admin API (§4 "Instance admin service"), whose queries are deliberately cross-org,
served only by the separate cluster-internal `backend/admin` service and gated by
`ADMIN_API_TOKEN` instead of a session.

```
orgs           id, name, slug, auto_join_domain (nullable), created_at
users          id, name, email (unique), status, created_at
org_members    org_id, user_id, role (OWNER|MEMBER), created_at        [PK org_id+user_id]
invites        id, org_id, email, invited_by, status, created_at

links          id, org_id, user_id (nullable — NULL for API-key-created links),
               api_key_id (nullable), code (unique), destination_url,
               type (PUBLIC|PRIVATE), status (ACTIVE|DISABLED_ABUSE — re-scan flag, §8),
               utm_source/medium/campaign (as-created/edited values),
               deeplink_config (JSON, nullable), visits, last_visit_at, created_at
link_rules     id, org_id, link_id, target_name, rule (JSON: conditions + url), created_at
link_edits     id, org_id, link_id, user_id, old_url, new_url, created_at
               (audit trail of destination edits — one row per applied PATCH)

clicks         id, org_id, link_id, ts, utm_source, utm_medium, utm_campaign,
               device_type, mobile_os, browser, browser_grade, referrer, ip, city,
               country, region, is_deeplink, target_matched, custom_tag_id (nullable)

api_keys       id, org_id, name, key_hash (SHA-256 hex, unique), key_hint,
               status (ENABLED|DISABLED), created_at, last_used_at

domains        id, org_id, hostname (unique — lowercase, punycode-normalized),
               verification_token (random base62, 32 chars), status (PENDING|VERIFIED|DISABLED),
               verified_at (nullable), is_primary (tinyint, single primary per org), created_at

abuse_reports  id, org_id, link_id, code, reason (≤140), reporter_contact (nullable),
               created_at   (public link reporting, §8 "URL scanning & abuse defenses";
               org/link derived server-side from the reported code)
```

**Foreign keys**: declared on all relationship columns, including `clicks.link_id` — data
integrity wins until click-insert throughput measurably says otherwise (revisit then, not
before).

Key schema decisions:

- **`code` is one namespace** — generated codes and custom aliases live in the same unique
  column, so they can never collide (INV-5). Setting a custom alias updates `code`; the old
  generated code stops resolving.
- **Codes are random base62, length 7** (~3.5 trillion values), retried on collision. Never
  derived from row IDs — links must not be enumerable (INV-5).
- **QR codes are not stored.** Generated on demand from `code` — the QR is a deterministic
  function of the short URL, so storing rendered PNGs would be pure bloat.
- **Deep-link config is a column on `links`, owner-written only** — never writable from the
  resolution path (INV-3).
- **`clicks` is written for every resolution** (INV-4), indexed on `(org_id, link_id, ts)`
  for per-link analytics and `(org_id, ts)` for org-wide usage metering (§8 "Extension seam:
  LimitsPolicy" — current-month event counts).

## 3. Short-code rules

- Charset `[a-zA-Z0-9]` generated / `[a-zA-Z0-9_-]{3,50}` for custom aliases.
- Reserved-word blacklist for aliases (they share the root path with service routes):
  `api`, `login`, `metrics`, `favicon.ico`, `robots.txt`, `.well-known`, plus anything starting
  with a dot. Keep the list in one Go slice; validation error is 422.
- Alias conflicts → HTTP 409.

## 4. HTTP API

Principles: REST resources under `/api/v1`; **org is always inferred from auth context, never
from the path**; Gofr wraps responses in `data`; errors use Gofr conventions.

### Auth & session

The SPA asks the server which sign-in methods exist (`AUTH_PROVIDERS`, §5) and renders them.
For Google it exchanges a client-side Google ID token; for Microsoft it runs the PKCE
authorization-code flow in the SPA and submits the resulting ID token; for the dev provider
(local evaluation only) it submits a plain name+email. Either way ogtr issues its own JWTs (access +
refresh) — the provider credential is only an identity proof at the boundary.

```
GET  /api/v1/auth/providers                         → {providers, google_client_id} (public)
POST /api/v1/auth/google      {id_token}            → {access_token, refresh_token, user, orgs}
POST /api/v1/auth/dev         {email, name}         → same shape; 404 unless "dev" is enabled
POST /api/v1/auth/refresh     {refresh_token}       → {access_token, refresh_token}
GET  /api/v1/me                                     → current user + org memberships
POST /api/v1/auth/switch-org  {org_id}              → new token pair scoped to that org
```

A login route for a provider not in `AUTH_PROVIDERS` answers **404** (enforced in the handler —
same effect as an unregistered route, but unit-testable). `/auth/dev` validates email format
and non-empty name with **422**. `google_client_id` is served to the SPA (client IDs are public
identifiers) and takes precedence over its build-time `VITE_GOOGLE_CLIENT_ID` fallback.

**Active org lives in the access token** (`org_id` claim). A user in multiple orgs switches by
requesting a re-scoped token; every other endpoint just trusts the claim. First login with no
org membership: if the email's domain matches some org's `auto_join_domain`, auto-join as
MEMBER; otherwise the response signals "no org" (`orgs: []`, `active_org_id: 0`) and the SPA
offers org creation.

**Session tokens** (implemented, phase 2): both access and refresh tokens are stateless HS256
JWTs signed with `JWT_SIGNING_KEY`. Claims: `user_id`, `org_id` (0 = no active org), `role`,
`typ` (`access`|`refresh` — one can never be replayed as the other), plus standard
`iss=ogtr`/`sub`/`iat`/`exp`. Lifetimes default to **15m access / 30d refresh**,
overridable via `ACCESS_TOKEN_TTL` / `REFRESH_TOKEN_TTL`. Stateless refresh was chosen over a
refresh-token table: no session store to scale or migrate, and the security gap is covered by
refresh re-validating the user's status and org membership in the DB — a removed member's next
refresh silently downgrades to an org-less pair, and a disabled user gets 401. (Instant
server-side revocation is the one thing this can't do; revisit only if that requirement
appears.)

Login also auto-converts any pending invites for the verified email into MEMBER memberships
(invite → `ACCEPTED`) before resolving the active org. `POST /api/v1/orgs` returns the new org
*plus* a token pair already scoped to it (creator = OWNER), so the SPA needs no follow-up
switch-org call. OWNER-only operations check the role in the DB, not the (≤15m stale) token
claim. Removing the last OWNER of an org is rejected (422), which also covers an OWNER
removing themselves.

### Orgs & membership

```
POST   /api/v1/orgs                    create org (creator becomes OWNER)
GET    /api/v1/org                     current org details
PATCH  /api/v1/org                     update name / auto_join_domain (OWNER)
GET    /api/v1/org/members             list members
DELETE /api/v1/org/members/{userId}    remove member (OWNER)
POST   /api/v1/org/invites             invite by email (OWNER)
GET    /api/v1/org/invites             pending invites
DELETE /api/v1/org/invites/{id}        revoke invite
```

(`/org` singular = "the org in my token".)

`POST /api/v1/orgs` consults the deployment's `limits.Policy` (§8 "Extension seam:
LimitsPolicy"); a denial responds `403` with error code `LIMIT_REACHED`. The default policy
never denies.

### Links

```
POST   /api/v1/links                    shorten {url, type?, utm_source?, utm_medium?, utm_campaign?}
                                        → link incl. short_url; dedupes per org on destination URL
GET    /api/v1/links?page=              my links, paginated (10/page), incl. last_visit, visits
GET    /api/v1/links/{id}               link detail
PATCH  /api/v1/links/{id}               {url, utm_source?, utm_medium?, utm_campaign?}
                                        edit destination (creator or org OWNER; audited)
PUT    /api/v1/links/{id}/alias         {alias} set custom short code (409 on conflict)
PUT    /api/v1/links/{id}/deeplink      {android:{...}, ios:{...}} owner-set deep-link config
GET    /api/v1/links/{id}/qr            QR code as image/png (Cache-Control: immutable)
```

**Link semantics** (implemented, phase 3): schemeless input is auto-prefixed
**https://** (https is the right default for new links). Explicit UTM values are
baked into `destination_url` at creation via `net/url` and also stored
as-created on the link row. Dedupe (and the already-short lookup) only matches
links the *caller can see* — org-scoped, and another user's PRIVATE link never
comes back from a dedupe hit, so its code is not leaked. PRIVATE links are
creator-only in list/detail (a non-creator gets 404, not 403 — existence is
not revealed). POST /api/v1/links returns **201 in the dedupe case too**:
Gofr derives success status purely from the HTTP method, so "existing link"
is signaled by the returned id/code, not by a 200. QR PNGs are generated with
`github.com/skip2/go-qrcode` (512px, medium error correction).

**Destination editing** (FEATURES.md §1.5): `PATCH /api/v1/links/{id}` repoints
a link. Validation reuses the creation path (normalize scheme, 422 on
malformed, own short domain rejected; UTMs baked into the destination and
stored as the link's UTM columns); dedupe is creation-time-only. Permission is
link creator **or** org OWNER — the role is read from the DB like org
management, never from the (≤15m stale) token claim; other members get 403,
while visibility failures (cross-org id, another user's PRIVATE link) stay
404. Every applied edit writes a `link_edits` audit row in the same flow; a
no-op edit (same resulting destination) writes nothing. The SPA offers the
edit modal from the links-list kebab menu and the link-detail header.

### Target rules

```
POST   /api/v1/links/{id}/rules         add rules [{target_name, device_type, location, url}]
GET    /api/v1/links/{id}/rules         list
PUT    /api/v1/rules/{ruleId}           edit
DELETE /api/v1/rules/{ruleId}           delete
GET    /api/v1/cities?q=                city-name autocomplete for the rule builder
```

### Analytics

```
GET /api/v1/links/{id}/stats?from=&to=&deeplink=   full per-link report (FEATURES.md §5.1)
GET /api/v1/stats/unique-clicks?link_ids=1,2,3     distinct-tag click count (§5.2)
GET /api/v1/stats/tags                             distinct custom tag IDs (§5.3)
GET /api/v1/stats/utm                              UTM source/medium/campaign analysis (§6.3)
```

Dates `YYYY-MM-DD`; default window last month; `from > to` → 400.

**Implementation notes** (phase 5):

- **One response, typed models**: the per-link report is a single
  `models.LinkStatsReport` — `total_clicks`, `clicks_per_day` (`{date, clicks}`),
  `per_day_breakdowns` / `total_breakdowns` (each `{browser, device_type,
  referrer, mobile_os, location}`; `mobile_os` excludes `'NA'` desktop rows;
  `location` is shaped `{countries, regions, cities}` — the three levels of
  the recorded GeoIP data, with NULL geo values COALESCEd to `Unknown` in SQL
  and ties ordered by the labeled value), `deeplink` (`deeplink_clicks` =
  count of clicks with `is_deeplink` equal to the `?deeplink=` flag;
  `mobile_app_opens` always counts `is_deeplink = TRUE`), `target_rule`
  (`{total_clicks, target_matched}`, **null when the link has no rules**),
  and `clicks` (the detailed list, newest first). Report visibility mirrors
  link detail: org-scoped, PRIVATE → creator-only 404.
- **Date range**: inclusive of both end days — `ts >= from AND
  ts < to + 1 day`, bound as `YYYY-MM-DD` strings. Per-day buckets GROUP BY the
  selected `DATE_FORMAT(ts, '%Y-%m-%d')` expression itself: grouping by
  `DATE(ts)` while selecting `DATE_FORMAT` trips MySQL's `only_full_group_by`.
- **UTM analysis** is scoped to links the *viewer* can see (org + PUBLIC-or-own,
  same `visibleFilter` as link listing), so another user's PRIVATE link never
  leaks through aggregate UTM rows. Empty/NULL UTM values are skipped in SQL.
- `/stats/tags` returns the full distinct set in one query (no paginated
  loop); `/stats/unique-clicks` caps `link_ids` at 100 and builds the
  `IN` list from placeholders only.
- **UI charts are hand-rolled** (Tailwind divs: column chart + horizontal bar
  lists, hover tooltips, zero-filled day axis up to 92 days) — no chart
  library, consistent with the minimal-deps rule.

### Custom domains

```
POST   /api/v1/org/domains                {hostname} register → PENDING + TXT record to publish
GET    /api/v1/org/domains                list with status + TXT instructions (any member)
POST   /api/v1/org/domains/{id}/verify    run the DNS ownership check
PUT    /api/v1/org/domains/{id}/primary   make this the org's single primary (VERIFIED only)
DELETE /api/v1/org/domains/{id}           remove (short-URL display reverts to SHORT_DOMAIN)
```

Mutations are OWNER-only (role read from the DB, like org management); members may list.
Design decisions (implemented):

- **Hostname validation**: lowercase + punycode-normalized (`golang.org/x/net/idna`, strict
  LDH labels); no scheme, path, port or IP literal; at least two labels. The deployment's own
  `SHORT_DOMAIN` — or any subdomain of it — can never be registered/verified (**422**); this is
  re-checked at verify time since `SHORT_DOMAIN` can change between deployments. Hostnames are
  **globally unique** across the deployment (one org owns a hostname, backed by a unique
  index): a hostname registered by any org is a **409**, with a message that does not reveal
  which org holds it.
- **Verification** is a DNS ownership proof: a TXT record at `_ogtr-verify.<hostname>` whose
  value equals the domain's random 32-char token. The check goes through a `DNSResolver`
  interface (`LookupTXT`); production wires `net.DefaultResolver`, tests substitute a mock —
  the same verification code runs in both, **no bypass path**. Lookups are bounded (5s
  timeout). **Verify semantics**: `200` with the VERIFIED domain on success (`verified_at`
  stamped; idempotent for an already-verified domain); **409** with a human-readable reason
  whenever DNS did not prove ownership — record missing/not propagated, value mismatch, lookup
  error or timeout. 409 was chosen over 4xx alternatives because the request is well-formed
  and authorized; it conflicts with the *current state of the world* (DNS), and the client's
  remedy is to fix DNS and retry.
- **Primary**: `is_primary` marks the org's display domain. The swap is transactional (clear
  the org's primary + set the new one in one transaction; the SET statement requires
  `status = 'VERIFIED'`, so a concurrent unverify rolls the whole swap back). A non-VERIFIED
  domain is **422**.
- **Short-URL display**: when an org has a primary VERIFIED domain, `short_url` in every API
  response (create/dedupe/list/detail) is `https://<primary-domain>/<code>`, and the QR
  endpoint encodes the same URL. Custom domains always display **https** regardless of
  `SHORT_SCHEME` — TLS for them is an operator/ingress concern (DEPLOYMENT.md §5). Without a
  verified primary the display stays `SHORT_SCHEME://SHORT_DOMAIN/<code>`. The display base
  is resolved once per request (one extra indexed query; per-page for lists) and a lookup
  failure falls back to `SHORT_DOMAIN` — display never blocks link management. Deleting the
  primary domain just reverts the display; codes and analytics are untouched.

**Host-aware resolution** (redirect pipeline, see "Resolution & redirect"): the request Host
(captured by the visitor middleware — gofr handlers can't read headers — and **port-stripped**,
since local dev sends `localhost:5810`) classifies every resolution:

| Request Host | Behavior |
|---|---|
| `SHORT_DOMAIN`, `localhost`/`127.0.0.1`/`::1`, or no Host | Global namespace — any org's code resolves (exactly the pre-custom-domain behavior) |
| A VERIFIED custom domain | Resolves ONLY the owning org's links; another org's code → 404 (same 404 as unknown — existence stays hidden) |
| PENDING/DISABLED domain, or any unknown hostname | 404 |

Bare `/` on a custom domain is **404** — the `WEBSITE_URL` bounce is SHORT_DOMAIN-only.
Unknown hosts deliberately 404 rather than bounce to `WEBSITE_URL`: whatever DNS someone
points at the deployment must not become a marketing redirect. The `SHORT_DOMAIN` URL keeps
resolving **every** link regardless of custom domains — the code namespace stays global and
deployment-wide (INV-5 unchanged); a custom domain only adds a scoped entry point and changes
the *displayed* URL. `GET /api/v1/resolve` applies the same host scoping.

### API keys

```
POST   /api/v1/api-keys          create → returns plaintext key once; stored as hash
GET    /api/v1/api-keys          list (no key material beyond the hint)
DELETE /api/v1/api-keys/{id}     disable (204; keys are never hard-deleted)
```

API-key auth: `X-API-Key` header accepted on `POST /api/v1/links` and `GET /api/v1/resolve`.
The key resolves to its org; links created this way record `api_key_id` (developer
attribution).

Implementation notes (phase 6):

- **Key format**: `slk_` + 40 crypto/rand base62 chars (~238 bits). Only the SHA-256 hex
  digest is stored (`key_hash`, unique); the plaintext appears exactly once, in the create
  response. `key_hint` (`slk_` + first 8 random chars) is stored for the list UI — enough to
  recognize a key, useless to reconstruct it.
- **Middleware split**: on the two key-auth routes only, a present `X-API-Key` header makes
  the auth middleware skip JWT and forward the raw key on the request context; the handler
  authenticates it (the hash lookup needs the DB, which the middleware doesn't have). The
  header is ignored everywhere else — key management itself always requires a JWT. When both
  a key and a bearer token are sent on those routes, the key wins.
- **`links.user_id` is nullable**: API-key-created links have no user (`user_id` NULL,
  `api_key_id` set) rather than a fake sentinel user. The dashboard shows them as created
  "via API".
- **PRIVATE requires a real user**: a PRIVATE link needs a creator to be private to, so
  key-authenticated shortens with `type=PRIVATE` are 422 — API-created links are always
  PUBLIC.
- **Resolve fails loudly on a wrong key**: `/api/v1/resolve` stays public, but an explicitly
  supplied invalid/disabled key is 401 rather than silently proceeding as anonymous. A valid
  key changes nothing about resolution; it only stamps `last_used_at`.
- **`last_used_at` is fire-and-forget**: stamped on successful key auth in a detached write
  (context.WithoutCancel) — the request never waits on, or fails because of, the bookkeeping.
- **Any org member manages keys** (create/list/disable): the spec is silent on roles, so the
  simplest rule wins. Cross-org key IDs are 404 — existence stays hidden.
- **Disable, never delete**: disabled keys stop authenticating immediately (401) but stay
  listed and keep `api_key_id` attribution on the links they created. "Rotation" =
  create a new key, disable the old one.

### Instance admin service

The operator surface of a deployment: a self-hoster (or the operations console of a hosted
deployment) administering their own instance. Open-core functionality — instance
administration, not commercial logic. Served by the SEPARATE `backend/admin` service
(deployed as `ogtr-internal`, cluster-internal only) — the public server (`backend/server`)
registers none of these routes.

```
GET  /api/internal/users?query=&page=        users across all orgs (25/page) + their org
                                             memberships and roles, created_at and
                                             last_active_at (null = never seen; touched on
                                             login/refresh, throttled to 1 write/hour);
                                             query matches email/name
GET  /api/internal/orgs?query=&page=         orgs (25/page) with aggregate counts: members,
                                             links, clicks_30d, domains — plus owner
                                             {id,email,name} (first OWNER by join time; null
                                             when ownerless); query matches name/slug
GET  /api/internal/orgs/{id}/users           one org's full member list {id, email, name,
                                             role, joined_at, last_active_at}, OWNERs first
                                             then join order; 404 for an unknown org
GET  /api/internal/reports?page=             abuse reports newest-first (25/page), each joined
                                             with the reported link's live status + destination
GET  /api/internal/links/{id}                operator link detail (any org): row + org_name +
                                             creator_email (null for API-key-created links)
POST /api/internal/links/{id}/disable        {reason?} → status DISABLED_ABUSE (410 page, no
                                             clicks — identical to a re-scan disable)
POST /api/internal/links/{id}/enable         status back to ACTIVE
GET  /api/internal/stats/daily?days=30       per-day {date, signups, links_created, clicks}
                                             (UTC days, zero-filled; default 30, max 90)
```

Design decisions:

- **A separate service, not routes on the public server.** The cross-org admin surface must
  never share a listener with internet-facing traffic. `backend/admin` is its own
  `package main` (same Go module, reusing the shared `stores`/`services`/`handlers`
  packages), deployed as the `ogtr-internal` ClusterIP service and NEVER exposed on any
  ingress. **The primary access control is network isolation** — only in-cluster callers
  (e.g. an operations console's gateway) can reach it at all; the token gate below is
  defense in depth. Naming nuance: the component directory is `backend/admin` because
  `internal/` is Go's reserved import-restriction directory name, but the
  deployment/service name stays `ogtr-internal` (the internal-service convention) and the
  route paths stay `/api/internal/*` (a wire contract consumers depend on).
- **INV-6 carve-out.** These endpoints are DELIBERATELY cross-org — the one sanctioned
  exception to "org-scoped everything". The compensating controls: the admin service is
  network-isolated, nothing under `/api/internal/` is reachable without the deployment's
  `ADMIN_API_TOKEN`, and all the cross-org queries live in one file (`stores/admin.go`) so
  the exception stays auditable.
- **Config-gated dark** (the dev-provider pattern): `ADMIN_API_TOKEN` unset — the default —
  makes every admin endpoint answer **404**. A stock deployment that doesn't run
  `ogtr-internal` has no admin surface at all; one that runs it without the token behaves
  as if the API did not exist.
- **Token gate, not JWT**: the service's `AdminTokenGate` middleware (`backend/admin`)
  guards the `/api/internal/` prefix; the request's `X-Admin-Token` header must equal
  `ADMIN_API_TOKEN` (constant-time comparison over SHA-256 digests, so neither content nor
  length leaks through timing). A missing or wrong token is also **404, not 401** — a prober
  without the token cannot distinguish a deployment that has the API enabled from one that
  does not. There is no JWT middleware on this service — it has exactly one concern. On the
  public server, `/api/internal/*` is an ordinary unknown `/api` path (401 without a
  session, 404 with one) — no route, no exemption, nothing to probe.
- **Schema ownership stays with the public server**: `backend/admin` runs no migrations; it
  reads and writes the same MySQL database whose schema `backend/server` migrates at boot.
- **Disable/enable reuse the abuse-status machinery**: the same `SetStatusByID` write the
  periodic re-scan uses. A disabled link serves the 410 warning page and records no clicks;
  row, code and analytics survive. Enable is the API form of the operator re-enable that used
  to be a manual DB action (which still works). Both actions are logged with their reason via
  the gofr logger — audit-light v1; an audit table is future work.
- **Counts are grouped queries, never N+1**: org aggregate counts (members/links/clicks_30d/
  domains) and per-user org memberships are each one grouped `IN (...)` query per page.
  `clicks_30d` is served by the clicks `(org_id, ts)` index.
- **`provider_hint` is omitted** from the users listing: the core does not record which
  identity provider a user signed in with, so there is nothing to derive it from cheaply.
  (If sign-in provenance is ever stored, the field can be added additively.)

### Resolution & redirect

```
GET /{code}                       302 → destination. Public, no auth. Records click.
GET /api/v1/resolve?code=&tag=    JSON resolution (programmatic). Records click.
                                  `tag` = custom tag ID attached to the click.
GET /                             bare-domain bounce: 302 → WEBSITE_URL when configured
                                  (404 when unset). Public; records no click.
```

- **302, not 301**, with `Cache-Control: no-store` — a cached 301 silently kills click tracking.
- Resolution pipeline (single implementation shared by both endpoints):
  0. Classify the request Host (§4 "Custom domains"): SHORT_DOMAIN/loopback → global; a
     VERIFIED custom domain → scope to its org; anything else → 404.
  1. Look up `code` → link (404 if absent, or outside the host's org scope).
  2. Build visitor environment (device, OS, browser, IP, referrer; city via GeoIP if loaded).
  3. Deep link: if link's `deeplink_config` matches visitor OS → serve app intent (Android
     intent URI with fallback / iOS link).
  4. Target rules: first rule whose conditions (`is`/`is_not` over OS and city, all must match)
     pass → its URL.
  5. UTM: explicit UTMs already baked into destination at creation; otherwise auto-tag
     `utm_source` (referrer-derived; direct traffic falls back to `UTM_SELF_SOURCE`,
     defaulting to `SHORT_DOMAIN`) and `utm_medium`
     (device-derived) onto the outgoing URL — built with `net/url`, never string concat.
  6. Update `visits`/`last_visit_at`; insert `clicks` row (allowlisted columns only — INV-2).
- Click recording must never block the redirect on GeoIP/parse failures: degrade fields to
  empty, still record. Counter/click write errors are logged and swallowed — the visitor is
  redirected regardless.

**Implementation notes** (phase 3):

- **Redirect routing**: the root route is registered as
  `GET /{code:[a-zA-Z0-9_-]+}` (gorilla/mux regex). The charset excludes dots,
  so `/favicon.ico`, `/robots.txt` and `/.well-known/*` — which gofr registers
  at `Run()`, *after* user routes — are never shadowed (verified empirically).
  `/api` as a bare segment matches the pattern but `api` is a reserved word,
  so it can only ever 404.
- **Visitor environment**: gofr handlers cannot read headers/RemoteAddr, so a
  small middleware (`visitor.Middleware`) captures User-Agent / Referer /
  X-Forwarded-For / RemoteAddr / Host on resolution paths (and Host on the
  bare root, for the custom-domain rules) and stashes a parsed `Env` on the
  request context. UA parsing is an in-house string bucketer with a
  fixed bucket set (Mobile/Tablet/Desktop, iOS/Android/Windows/Other/NA,
  Chrome/Safari/Firefox/Opera/IE/Other) — no UA-parser dependency.
- **Cache headers**: gofr's `response.Redirect`/`response.File` cannot carry
  headers, so `handlers.CacheControl` middleware sets `no-store` on the
  redirect path and `immutable` on QR responses (200s only).
- `GET /api/v1/resolve` is in the auth middleware's exempt list (resolution
  never requires login); it accepts `&tag=` for campaign-tagged clicks.

**Implementation notes** (phase 4 — targeting):

- **Evaluation order**: deep link first, then target rules — a
  matching deep link swaps the outgoing URL for the app intent, and a
  matching target rule overrides that again; `is_deeplink` and
  `target_matched` are both recorded on the click regardless of which URL
  won. Rules evaluate in creation order (`ORDER BY id`), first match wins.
  Deep-link and rule URLs are served exactly as configured (no UTM
  auto-tagging); the click still records the default destination's UTM set.
- **Unknown city**: when GeoIP is disabled or the IP resolves to no city, a
  location condition evaluates false — *including* `is_not` — so an unknown
  location never triggers a location-conditioned rule.
- **Rule storage**: `link_rules` keeps conditions + destination in the `rule`
  JSON column, with `target_name` mirrored into its own column. JSON column
  parameters must be bound as `string`, not `[]byte` — the MySQL driver sends
  `[]byte` with the binary charset, which JSON columns reject (error 3144).
  Same applies to `links.deeplink_config`.
- **gofr gotcha**: a handler returning a typed-nil slice (e.g.
  `[]models.Rule(nil)`) through the `any` return makes gofr respond
  206 "partial content" instead of the error's status code (typed-nil
  *pointers* are detected fine) — slice-returning handlers convert errors to
  an untyped `nil` return.
- **Targeting never blocks resolution**: a rule-listing failure is logged and
  rules are skipped; the visitor is still redirected.

## 5. Auth provider boundary

```go
// backend/auth/provider.go
type IdentityProvider interface {
    // Verify checks a provider-issued credential and returns the proven identity.
    Verify(ctx *gofr.Context, credential string) (Identity, error)
}
type Identity struct{ Email, Name string }
```

Which providers are live is deployment config: `AUTH_PROVIDERS` (comma-separated; default
`google`; unknown values refuse to start). Providers:

- **`google`** — `GoogleProvider` verifies Google ID tokens: RS256 signature against the JWKS,
  issuer, audience = `GOOGLE_CLIENT_ID`, expiry, `email_verified`. The JWKS URL is configurable
  (`GOOGLE_JWKS_URL`, defaulting to Google's published set) so tests and local E2E can point at
  a local JWKS server — the verification logic is identical in tests and prod; there is no
  hidden bypass inside it.
- **`microsoft`** — `MicrosoftProvider` verifies Microsoft identity platform (v2.0) ID
  tokens from the multi-tenant `common` endpoint (work/school **and** personal accounts):
  RS256 signature against the JWKS (`MICROSOFT_JWKS_URL`, defaulting to the published
  `common` v2.0 key set, overridable only so tests can run a local JWKS server — same
  no-bypass rule as Google), audience = `MICROSOFT_CLIENT_ID`, expiry/not-before, and the
  **per-tenant issuer pattern**: the issuer must be exactly
  `https://login.microsoftonline.com/{tid}/v2.0` with `{tid}` equal to the token's own `tid`
  claim (checked structurally — exact prefix, exact suffix, tid segment match). Identity
  email is the `email` claim, falling back to `preferred_username` only when it parses as a
  bare email address (it can legally be a phone number or non-routeable UPN). The SPA obtains
  the ID token itself via a hand-rolled PKCE authorization-code flow (no MSAL dependency).
- **`dev`** — `DevProvider`, the zero-setup evaluation path: accepts any
  well-formed email + non-empty name with **no credential proof** (the "credential" is the
  JSON-encoded email/name pair from `POST /auth/dev`). Exists so a fresh install can be tried
  without creating a Google OAuth client. It is explicit and opt-in — disabled by default,
  its endpoint 404s when disabled, and the server logs a prominent WARN at boot when it is
  enabled. Never enable it in production; there is still no *hidden* auth-bypass path.
- **Future enterprise IdP providers** (e.g. other OIDC/SAML identity services) slot in behind
  the same interface — no other code changes.

Everything after `Verify` (JIT user creation, invite conversion, org resolution, ogtr JWT
issuance, refresh) is provider-independent — the dev flow exercises the exact same login path
as Google.

JIT provisioning (FEATURES.md §7.2): unknown email → create `users` row on first successful
verify.

## 6. GeoIP

- Optional. `GEOIP_DB_PATH` config; when set, the MaxMind GeoLite2-City `.mmdb` is opened
  **once at startup** and shared — never reopened per lookup. The handle is deliberately never
  closed — it lives exactly as long as the process.
- One lookup per click resolves city, region (first subdivision) and country together
  (`geo.Location`): the city feeds target-rule matching, and all three are recorded on the
  click for the location analytics breakdown (FEATURES.md §5.1).
- City autocomplete (`GEOIP_CITIES_CSV`, independent of the `.mmdb`): the locations CSV is
  read **once at startup** into a sorted, deduplicated in-memory slice of city names
  (~100k unique names, a few MB) with a pre-lowered copy for case-insensitive prefix scans;
  never re-read per request. `/api/v1/cities?q=` returns up to 20 matches alphabetically.
- When unset/missing: location rule conditions evaluate to no-match, `clicks.city`/
  `country`/`region` stay empty (the location breakdown reports everything as `Unknown`),
  `/api/v1/cities` returns 501. Everything else works. A path that is set but unloadable logs
  an error and degrades the same way — it never blocks startup.
- Ship the `.mmdb` and CSV from the **same GeoLite2 release**: the rule-builder autocomplete
  draws names from the CSV while resolution matches the `.mmdb`'s city name, and vintages can
  disagree (e.g. older "Bangalore" vs newer "Bengaluru"), making a picked city never match.
- The `.mmdb` and city-names CSV are **not committed** (MaxMind license + size). Deployment
  mounts them (init container / volume) using the operator's own MaxMind license key;
  documented in DEPLOYMENT.md.

## 7. Configuration (env, loaded by Gofr)

Public server (`backend/server`, config in `backend/server/configs/`):

```
HTTP_PORT / METRICS_PORT          5810 / 5811 locally (see LOCAL_DEVELOPMENT.md ports table)
DB_HOST DB_PORT DB_USER DB_PASSWORD DB_NAME DB_DIALECT=mysql
SHORT_DOMAIN                      e.g. links.example.com  (link generation + already-short detection)
SHORT_SCHEME                      https (default)
APP_URL                           SPA origin, for CORS + OAuth redirect
AUTH_PROVIDERS                    enabled sign-in methods: google, microsoft, dev (default google; §5)
GOOGLE_CLIENT_ID                  Google sign-in audience
GOOGLE_JWKS_URL                   Google signing keys (default: Google's; overridden in tests)
MICROSOFT_CLIENT_ID               Microsoft sign-in audience (Azure app client ID)
MICROSOFT_JWKS_URL                Microsoft signing keys (default: Microsoft's; overridden in tests)
JWT_SIGNING_KEY                   ogtr session tokens (required; server refuses to start without it)
ACCESS_TOKEN_TTL                  access-token lifetime (Go duration, default 15m)
REFRESH_TOKEN_TTL                 refresh-token lifetime (Go duration, default 720h)
UTM_SELF_SOURCE                   utm_source for direct-traffic auto-tag (default: SHORT_DOMAIN)
WEBSITE_URL                       optional bare-domain bounce target for GET / (unset: 404)
GEOIP_DB_PATH / GEOIP_CITIES_CSV  optional (§6)
ABUSE_CONTACT                     contact line on flagged errors / 410 page / preview (§8)
BLOCKLIST_FEED_URLS               comma-separated blocklist feeds (§8; unset: layer off)
BLOCKLIST_REFRESH_INTERVAL        feed refresh interval (default 6h)
WEBRISK_API_KEY                   Google Web Risk layer (§8; unset: layer off)
RESERVED_ALIASES                  extra reserved short-code words (comma-separated)
SHORTENER_DOMAINS                 extra shortener hosts refused as destinations
RESCAN_INTERVAL                   destination re-scan interval (default 24h; ≥24h = daily)
LINK_CREATE_RATE                  creates+edits per principal per minute (default 30)
```

Instance-admin service (`backend/admin`, config in `backend/admin/configs/`):

```
HTTP_PORT / METRICS_PORT          5820 / 5821 locally (the +20 secondary-backend slot)
DB_*                              the SAME database as the public server (no migrations here)
ADMIN_API_TOKEN                   admin token (§4 "Instance admin service"); unset (default)
                                  = every /api/internal/* endpoint 404s (the service is dark)
```

Each service's `configs/.local.env` is gitignored; ship `configs/.sample.env`. No real
secrets in git, ever.

## 8. Cross-cutting rules

- Health checks, metrics port, OpenAPI: Gofr built-ins; exempt from auth along with
  `POST /api/v1/auth/*` (google/dev/refresh), `GET /api/v1/auth/providers` and `GET /{code}`.
  Note: Gofr only starts the HTTP server (including
  health endpoints) once at least one route is registered.
- Gofr-style errors only; custom error types must implement `StatusCode()`. An error may
  additionally implement Gofr's `Response() map[string]any` to add machine-readable fields
  (e.g. `code`) to the error envelope.
- Migrations: Gofr migrations, timestamp prefix from `date +%Y%m%d%H%M%S`. Production
  databases exist, so every schema change lands as a NEW timestamped migration file (the
  pre-release edit-the-initial-file convention is retired); additive changes are folded into
  the initial schema too so a fresh install builds from one file, with the incremental file
  guarded to skip what already exists.
- Tests: handler/service/store layering with interfaces + gomock throughout.

### URL scanning & abuse defenses

Feature spec: FEATURES.md §10; operator guide: DEPLOYMENT.md §6. Settled
implementation decisions:

- **Scanner seam** (`backend/scanner`) mirrors the auth/limits seam
  style: a one-method interface (`Scan(ctx, url) (Verdict, error)`) with a
  `Pipeline` composing layers — `Syntactic` (always on, never errors: the
  floor), `FeedList` (BLOCKLIST_FEED_URLS; background startup load + gofr
  cron refresh; per-feed last-good on fetch failure), `WebRisk`
  (WEBRISK_API_KEY; 2s budget, fail-open). A layer error is logged and
  skipped — external services can add coverage but never block creation.
  Verdicts carry only a coarse category (`malware|phishing|abuse|policy`);
  the 422 message never reveals which rule/list matched (no testing oracle),
  and appends the ABUSE_CONTACT appeal line when configured.
- **Enforcement points**: `LinkService.shorten` (JWT and API-key paths) and
  `UpdateDestination`, scanning the normalized pre-UTM destination *before*
  dedupe. Plus a periodic re-scan (`RescanService`, gofr cron,
  RESCAN_INTERVAL): id-cursor batches over ACTIVE links with
  `last_visit_at` in the last 7 days, bounded per run; flagged links get
  `links.status = 'DISABLED_ABUSE'` (the two status writes are deliberately
  not org-scoped — the re-scan is a system job with no auth context; ids
  come from its own listing, never from clients). Re-enable = operator SQL
  only.
- **Disabled serving**: `GET /{code}` on a disabled link returns
  `response.File` (the inline HTML warning page) *together with* a
  410-status error — gofr renders File-with-error as "this body, that
  status". JSON resolve passes the 410 through as a normal error. No click
  is recorded for a disabled link (it did not resolve).
- **Preview** (`GET /{code}+`): the `+` is outside the redirect route's
  charset, so the two mux routes can never collide. Same host-scoping and
  guess-throttling as resolution via a shared `lookup`; records no click.
  Pages (410/preview/preview-404) are inline `html/template` documents —
  org-neutral, self-contained, auto-escaped.
- **Abuse reports** (`POST /api/v1/report`, auth-exempt): validates then
  rate-limits then looks up — an unknown code is an honest 404 (the page
  must say "no such link"; resolution already carries the guess throttle).
  Reports only insert triage rows; they never auto-disable.
- **Rate limits** (`backend/ratelimit`): in-memory sliding windows —
  creates+edits per principal (LINK_CREATE_RATE/min), reports per IP
  (5/min), and a resolver guess-throttle (>60 unknown-code 404s/min → 1min
  cooldown; successes never count). Per-instance by design (resets on
  restart, per-replica in k8s) — documented v1 trade-off, shared store on
  the roadmap. Client IPs come from the visitor middleware, which also
  covers the report/preview routes.
- **Reserved aliases** (`services/reserved.go`): one categorized Go slice
  set (auth-shaped / functional-infra / legal-brand, ~120 words) +
  RESERVED_ALIASES config additions. Full set on the shared domain;
  functional + additions only for orgs with a VERIFIED custom domain
  (`DomainStore.HasVerified`; a failed lookup falls back to the strict
  list). Generated codes are re-drawn when they hit the reserved set.

### Extension seam: LimitsPolicy

`backend/limits` defines the one extension point through which a deployment can bound
resource creation and analytics viewing:

- **`limits.Policy`** — an interface consulted before resource-creating actions and by the
  stats service. The app assembly (`backend/app`) wires **`limits.Unlimited{}`**, the
  default policy whose every check allows (and whose analytics window is unbounded), so a
  stock deployment behaves as if the seam did not exist. A deployment may substitute its own
  implementation via `app.WithPolicy` — every service that consults the seam receives the
  same instance.
- **Checks (axes)** and where each is enforced:

  | Check | Enforced at |
  |---|---|
  | `CanCreateOrg(ctx, userID)` | `POST /api/v1/orgs` (OrgService.Create) |
  | `CanCreateLink(ctx, orgID, userID)` | link creation only — the shared `shorten` path behind both the JWT and API-key routes (`userID` is 0 on the key path). Never on edits (alias / deep link / destination), and not on the already-short echo, which creates nothing |
  | `CanAddDomain(ctx, orgID)` | custom-domain registration (DomainService.Create) |
  | `CanAddMember(ctx, orgID)` | invite creation (OrgService.CreateInvite) AND the single membership-creating choke point on the login path (AuthService.joinAsMember, used by both invite acceptance and auto-join) |
  | `CanCreateAPIKey(ctx, orgID)` | API-key creation (APIKeyService.Create) |
  | `AnalyticsWindow(ctx, orgID) (Window, error)` | every stats entry point (see Window semantics below) |

  Each check runs after input validation and before any store access, so a denial writes
  nothing.
- **Forward compatibility** (gRPC-style): the interface grows additively. Implementations
  must embed **`limits.UnimplementedPolicy`** — enforced by an unexported interface method —
  and override only the checks they enforce. Un-overridden checks fall through to the base,
  which always allows, so an implementation written against today's interface keeps compiling
  and keeps its behavior when new checks are added.
- **Denial contract**: a check blocks an action by returning `limits.Deny("reason")`. The API
  responds `403` with `{"error": {"code": "LIMIT_REACHED", "message": "<reason>"}}` — the
  policy's message is passed through verbatim and the SPA shows it as a notice, so the core
  never hardcodes denial wording. Any other error from a check is an internal policy failure
  (500), not a denial.
- **Login-path exception**: on the login path a `CanAddMember` denial must never fail the
  login — the join is skipped (logged), a pending invite stays PENDING so it converts on a
  later login once the policy allows, and an auto-join candidate simply stays org-less.
- **`limits.Window` semantics** (analytics viewing bounds; the zero value is unbounded):
  - `ViewableEvents` (0 = unbounded) caps VIEWING, not recording: when the org's
    current-calendar-month event count (`usage.EventsThisMonth`, UTC months) *exceeds* the
    bound, every stats endpoint (per-link report, unique-clicks, tags, UTM analysis) answers
    `403 LIMIT_REACHED` with the window's `Message` (or `limits.DefaultWindowMessage` when
    unset). The SPA renders the message as a notice in place of the charts.
  - `Retention` (0 = unbounded) clamps how far back stats queries look: the per-link report's
    from-date is clamped to now−Retention (a range fully outside the window yields an empty
    report, not an error), and the org-level queries carry the cutoff as an always-bound
    `ts >= since` predicate (`1970-01-01` when unbounded, so the query shape is constant).
  - Both bound viewing only. Older events stay stored and clicks keep recording — lifting
    the window later makes history visible again.
- **Redirects never break** (FEATURES.md INV-7): the resolution/redirect path takes **no
  policy or usage dependency at all** — structurally, not by convention. `ResolveService` has
  no such field or constructor parameter, so no policy state can ever block a redirect or
  stop click recording; `services/resolve_policy_test.go` pins this with reflection over the
  struct and constructor.
- **Usage metering & composition** (`backend/usage`): **`usage.Reader`** exposes cheap
  org-scoped counters — `EventsThisMonth`, `LinksCreatedThisMonth`, `DomainsCount`,
  `MembersCount`, `APIKeysActiveCount` (all single indexed COUNTs; "this month" = current
  calendar month, UTC; events = click rows, per INV-4). The open core itself uses only
  `EventsThisMonth` (the stats service's viewable-events gate). The intended composition:
  a deployment's Policy implementation is **constructed with a `usage.Reader`**
  (`usage.NewStore()` in the composing deployment's main) and consults its counters inside each check — the core
  never couples the two. `EventsThisMonth` is served by the clicks `(org_id, ts)` index
  (added as the first incremental migration; the `(org_id, link_id, ts)` analytics index
  cannot serve an org-wide time range).

### Deployment composition

The whole application assembly lives in **`backend/app`**: `app.Run(opts...)` builds
configuration, migrations, stores, services, handlers, routes and cron jobs, then serves.
The stock public-server binary is just `backend/server/main.go` → `app.Run()` — with no
options, behavior is identical to the pre-package single-file main. (The instance-admin
service, `backend/admin`, does NOT go through `backend/app` — it is its own tiny assembly
with exactly one concern.) A deployment composes the same core with its own
additions through functional options:

| Option | Effect |
|---|---|
| `app.WithPolicy(limits.Policy)` | substitutes the deployment's policy for `limits.Unlimited{}` (see "Extension seam: LimitsPolicy" above) |
| `app.WithMigrations(map[int64]migration.Migrate)` | extra migrations merged after the core's (same `date +%Y%m%d%H%M%S` keys; a key collision refuses to start). All migrations share the one `gofr_migrations` table |
| `app.WithRoutes(func(*gofr.App, *app.Services))` | registers extra HTTP routes after every core route; receives `app.Services` — a deliberately tight surface (currently `Usage usage.Reader` and `Members app.RoleReader`) that grows additively when a real route needs more |
| `app.WithProviders(map[string]auth.IdentityProvider)` | adds identity providers on top of `AUTH_PROVIDERS` (additive; added names are login-enabled; a name collision replaces the built-in) |
| `app.WithAuthExemptPaths(paths...)` | exempts additional exact request paths (any method) from the auth middleware, for deployment endpoints that carry their own authentication (e.g. signature-verified callback receivers) |

A composing deployment lives in its own module with its own `main.go` importing
`github.com/opengittr/ogtr/backend` and calling `app.Run` with options; the core repo
never learns what was composed. Deployment-registered routes are ordinary gofr handlers:
they see the same auth middleware (claims on the context), the same error conventions
(`apierrors`, `StatusCode()`), and the shared `ctx.SQL`.

The SPA carries the equivalent seam in `frontend/src/ext/index.tsx`, a stub module a
deployment may overwrite at image build time to register extra routes, sidebar navigation
items, and an optional trailing link on LIMIT_REACHED notices. The stock build ships the
empty stub, so the app behaves as if the seam did not exist.
