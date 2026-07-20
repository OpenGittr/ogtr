# Deployment

ogtr deploys as two Docker containers on Kubernetes — `server`
(Gofr API + redirects) and `app` (dashboard SPA) — plus
an externally provisioned MySQL 8+. All manifests live in
[`k8s/`](../k8s/README.md); this document is the walkthrough.

## 1. Build the images

All images target **linux/amd64** (build stages run natively via
`$BUILDPLATFORM`, so cross-building from an ARM Mac is fast). No registry is
decided yet — substitute your own.

```sh
# Backend — migrations compiled in, run at boot. GeoIP NOT baked in (licensed).
docker build --platform linux/amd64 \
  -t <registry>/ogtr-server:<tag> backend

# Dashboard SPA — VITE_* baked into the bundle at BUILD time (publishable
# values, not secrets). VITE_API_URL = the short domain origin.
docker build --platform linux/amd64 \
  --build-arg VITE_API_URL=https://links.example.com \
  --build-arg VITE_GOOGLE_CLIENT_ID=<client-id>.apps.googleusercontent.com \
  -t <registry>/ogtr-app:<tag> frontend

docker push <registry>/ogtr-server:<tag>   # …and the app image
```

Serving details: the app image is built on
[gostatic](https://github.com/opengittr/gostatic), which serves the built
files with the right cache policy by default (`index.html` as
`Cache-Control: no-cache`, hashed `/assets/*` as immutable) and falls back to
`index.html` on client-side routes. No nginx anywhere.

## 2. Kubernetes walkthrough

Prerequisites: a cluster with an ingress controller, DNS control for your
two domains, MySQL 8+ reachable from the cluster (the DB user needs DDL
rights — the server migrates at boot).

```sh
# 1. Namespace
kubectl apply -f k8s/namespace.yaml

# 2. Secrets — fill in a private copy of the template, kept outside the
#    repo (never commit it)
cp k8s/secrets-template.yaml /path/outside/the/repo/secrets.yaml
$EDITOR /path/outside/the/repo/secrets.yaml
kubectl apply -f /path/outside/the/repo/secrets.yaml

# 3. Config — set your domains in configmap.yaml first (see §3)
kubectl apply -f k8s/configmap.yaml

# 4. Workloads — set your image references first
kubectl apply -f k8s/server-deployment.yaml
kubectl apply -f k8s/app-deployment.yaml

# 5. Ingress — set your domains + ingress class first (see §5)
kubectl apply -f k8s/ingress.yaml
```

Probes: liveness `/.well-known/alive`, readiness `/.well-known/health` (Gofr
built-ins; readiness checks MySQL, so pods only take traffic once the DB is
reachable). Prometheus scrape annotations point at the metrics port (2121).

## 3. Configuration reference

Every backend env var (ARCHITECTURE.md §7) and where it lives:

| Env var | Where | Notes |
|---|---|---|
| `APP_NAME` | configmap | `ogtr-server` |
| `LOG_LEVEL` | configmap | `INFO` (raise to `WARN` for quiet prod logs) |
| `HTTP_PORT` | configmap | `8000` in-container (local dev: 5810) |
| `METRICS_PORT` | configmap | `2121` in-container (local dev: 5811) |
| `DB_DIALECT` | configmap | `mysql` |
| `DB_HOST` | **secret** (`db-host`) | often an internal IP — treated as secret |
| `DB_PORT` | configmap | `3306` |
| `DB_USER` | **secret** (`db-user`) | needs DDL rights (boot migrations) |
| `DB_PASSWORD` | **secret** (`db-password`) | |
| `DB_NAME` | configmap | `ogtr` |
| `SHORT_DOMAIN` | configmap | short-link host, e.g. `links.example.com` |
| `SHORT_SCHEME` | configmap | `https` |
| `APP_URL` | configmap | SPA origin — OAuth redirect target |
| `ACCESS_CONTROL_ALLOW_ORIGIN` | configmap | = `APP_URL`; without it gofr CORS defaults to `*` |
| `AUTH_PROVIDERS` | configmap | keep it `google` — **never enable the `dev` provider in production** (it lets anyone sign in as any identity) |
| `GOOGLE_CLIENT_ID` | **secret** (`google-client-id`) | OAuth Web client; app domain must be an authorized JS origin |
| `GOOGLE_JWKS_URL` | — (unset) | defaults to Google's published JWKS; only tests override |
| `JWT_SIGNING_KEY` | **secret** (`jwt-signing-key`) | required — server refuses to start without it |
| `ACCESS_TOKEN_TTL` | configmap | Go duration, default `15m` |
| `REFRESH_TOKEN_TTL` | configmap | Go duration, default `720h` |
| `UTM_SELF_SOURCE` | configmap | `utm_source` for direct-traffic auto-tag; empty = defaults to `SHORT_DOMAIN` |
| `WEBSITE_URL` | configmap | bare-domain bounce: `GET /` 302s to your marketing/landing site (hosted anywhere); empty = 404 |
| `GEOIP_DB_PATH` | configmap | empty = GeoIP disabled; see §4 |
| `GEOIP_CITIES_CSV` | configmap | empty = city autocomplete 501; see §4 |
| `ABUSE_CONTACT` | configmap | contact shown on flagged errors, the 410 page and previews; see §6 |
| `BLOCKLIST_FEED_URLS` | configmap | comma-separated blocklist feeds; empty = layer off; see §6 |
| `BLOCKLIST_REFRESH_INTERVAL` | configmap | Go duration, default `6h`; see §6 |
| `WEBRISK_API_KEY` | **secret** (`webrisk-api-key`) | Google Web Risk lookups; empty = layer off; see §6 |
| `RESERVED_ALIASES` | configmap | extra reserved words, comma-separated; see §6 |
| `SHORTENER_DOMAINS` | configmap | extra shortener hosts refused as destinations; see §6 |
| `RESCAN_INTERVAL` | configmap | Go duration, default `24h` (≥24h runs daily); see §6 |
| `LINK_CREATE_RATE` | configmap | creates+edits per principal per minute, default `30`; see §6 |
| `TZ` | deployment env | pinned `UTC` (gofr's MySQL DSN uses `loc=Local`) |

Frontend build-time values (baked into the bundle, not runtime env):

| Build arg | Image | Value |
|---|---|---|
| `VITE_API_URL` | app | backend origin = `SHORT_SCHEME://SHORT_DOMAIN` |
| `VITE_GOOGLE_CLIENT_ID` | app | same client ID as the backend secret |

## 4. GeoIP volume (optional)

The MaxMind GeoLite2-City `.mmdb` (click geo + location rules) and
locations CSV (city autocomplete) are licensed — never in the image or repo.
Without them everything else works: clicks record no city, location rules
never match, `GET /api/v1/cities` returns 501.

To enable:

1. Download `GeoLite2-City.mmdb` and `GeoLite2-City-Locations-en.csv` with
   your own MaxMind license key — **both from the same GeoLite2 release**
   (city names drift between vintages; a mismatched pair makes
   autocomplete-picked cities never match).
2. Put them on any volume the pod can mount read-only, e.g. a PVC:
   ```sh
   kubectl -n ogtr create -f - <<'EOF'
   apiVersion: v1
   kind: PersistentVolumeClaim
   metadata: {name: ogtr-geoip, namespace: ogtr}
   spec: {accessModes: [ReadWriteOnce], resources: {requests: {storage: 1Gi}}}
   EOF
   # copy the files in via a helper pod, or bake an initContainer that
   # downloads them with your license key
   ```
3. Uncomment the `geoip` volume + mount in `k8s/server-deployment.yaml`
   (mount path convention: `/data/geoip/`), set in `configmap.yaml`:
   `GEOIP_DB_PATH=/data/geoip/GeoLite2-City.mmdb`,
   `GEOIP_CITIES_CSV=/data/geoip/GeoLite2-City-Locations-en.csv`,
   and raise the server memory limit to ~256Mi (the datasets are held in
   memory).
4. `kubectl -n ogtr rollout restart deployment ogtr-server`. The
   files are loaded **once at startup**; a missing/unreadable file logs an
   error and degrades — it never blocks boot.

## 5. Domains & TLS

Two hosts (ARCHITECTURE.md §1), one ingress:

| Host | Backend | Also appears in |
|---|---|---|
| `SHORT_DOMAIN` (e.g. `links.example.com`) | ogtr-server | configmap `SHORT_DOMAIN`, app image `VITE_API_URL` |
| `APP_DOMAIN` (e.g. `app.example.com`) | ogtr-app | configmap `APP_URL` + CORS, Google OAuth JS origins |

If your deployment has its own marketing site, host it wherever you like and
point `WEBSITE_URL` at it (or add your own ingress routing for it) — see
ARCHITECTURE.md §1.

The manifests are ingress-controller-agnostic apart from
`ingressClassName: nginx` and an example cert-manager annotation — swap both
for your cluster's equivalents. Any TLS provisioning works; the Ingress just
references a `ogtr-tls` secret (provide it yourself or let your issuer
create it). Keep the domain values in sync across `ingress.yaml`,
`configmap.yaml` and the app image build arg — a mismatch shows up as CORS
failures or wrong short-URLs.

Note the redirect semantics: the server answers `GET /{code}` with a **302
and `Cache-Control: no-store`**. Don't add response caching for the short
domain at a CDN/ingress layer — a cached redirect silently kills click
tracking.

### Per-org custom domains (DNS + TLS for operators)

Orgs can bring their own short-link hostname (FEATURES.md §1.6). The app
handles registration, TXT-record verification and host-aware routing; the
operator side is DNS + TLS:

1. **Verification TXT** (done by the org, at *their* DNS provider): a TXT
   record at `_ogtr-verify.<their-hostname>` with the value shown in the
   dashboard. Only needed until the domain shows VERIFIED; harmless to keep.
2. **Traffic**: the custom hostname must reach the `ogtr-server`
   service — a **CNAME to `SHORT_DOMAIN`** (or an A/AAAA record to the same
   ingress IP). The server routes by Host header, so nothing else changes
   server-side; the ingress, however, must actually forward that host (see 3).
3. **Ingress + TLS**: certificates for custom domains are an
   **ingress/operator concern — the app neither provisions nor serves
   them**. For a handful of known domains, add each hostname to
   `k8s/ingress.yaml` (a rule pointing at `ogtr-server`) and to the TLS
   section/certificate. For hosted deployments with self-serve custom
   domains you need per-domain certificate automation — e.g. an ingress
   with on-demand/HTTP-01 issuance per host or a certificate-management
   controller watching a hostname source. That automation is deliberately
   out of scope of this repo; only the DNS-verification and routing halves
   live in the app.
4. The custom domain serves **redirects only** (that org's links; its bare
   `/` is 404). `SHORT_DOMAIN` keeps serving every link and the API
   regardless — custom domains never replace it, so don't repoint
   `VITE_API_URL` or `SHORT_DOMAIN` at a customer's domain.

## 6. Abuse protection

The safety layer (FEATURES.md §10) is on by default; this section is the
operator reference for its configuration and duties.

### 6.1 Destination scanning

Every created or edited destination runs through: syntactic guards (always
on — IP literals, userinfo tricks, homograph labels, shortener chaining;
`SHORTENER_DOMAINS` appends hosts to the chaining list) → blocklist feeds →
Google Web Risk. A flagged destination is a 422; the user-facing message is
deliberately coarse and, when `ABUSE_CONTACT` is set, carries an appeal
line. **Set `ABUSE_CONTACT`** — it is the escalation path for false
positives and the address the 410/preview pages and SECURITY.md point at.

**Feeds** (`BLOCKLIST_FEED_URLS`, comma-separated; each feed is plaintext,
one host or URL per line, `#` comments). Recommended public values:

```
https://urlhaus.abuse.ch/downloads/text_online/            # malware URLs (abuse.ch)
https://openphish.com/feed.txt                             # phishing URLs
https://zerodot1.gitlab.io/CoinBlockerLists/hosts_browser  # cryptojacking hosts
```

Feeds load in the background at startup and refresh every
`BLOCKLIST_REFRESH_INTERVAL` (default 6h — respect the feeds' own update
cadence and terms). A failed fetch logs and keeps the last-good data; feeds
never block startup. Feed data lives in memory (the recommended set is tens
of MB; size the pod accordingly).

**Google Web Risk** (`WEBRISK_API_KEY`, from a Google Cloud project with
the Web Risk API enabled — treat as a secret). Adds Google's threat
intelligence on top; lookups have a 2s budget and **fail open** (an outage
or quota exhaustion never blocks link creation — the local layers are the
floor). Mind the API's pricing/quota against your creation volume.

### 6.2 Re-scan and disabled links

Every `RESCAN_INTERVAL` (default 24h; values ≥24h run once daily) the
server re-scans destinations of links clicked in the last 7 days, in
bounded batches. Flagged links flip to `status = 'DISABLED_ABUSE'`: the
short link answers **410** with an org-neutral warning page, JSON resolve
answers 410, and analytics/history stay intact.

**Re-enabling is a manual operator action** — deliberate, because feeds
fluctuate and nothing should silently flip a disabled link back on. After
verifying a false positive:

```sql
UPDATE links SET status = 'ACTIVE' WHERE code = '<code>';
```

(Use the usual temporary MySQL client pod against the production DB.) The
next re-scan may disable it again if the destination is still listed —
clear the underlying listing first.

### 6.3 Abuse reports

`POST /api/v1/report` (public, 5/min per IP) and the `+`-suffix preview
page file rows into the `abuse_reports` table (org, link, code, reason,
optional reporter contact, timestamp). **Reports never auto-disable a
link** — triage them yourself:

```sql
SELECT r.created_at, r.code, r.reason, r.reporter_contact, l.destination_url
FROM abuse_reports r JOIN links l ON l.id = r.link_id
ORDER BY r.id DESC LIMIT 50;
```

Disable a confirmed-bad link with
`UPDATE links SET status = 'DISABLED_ABUSE' WHERE code = '<code>';`.

### 6.4 Rate limits (per-instance!)

Three in-memory sliding-window limiters: link creates+edits per user/API
key (`LINK_CREATE_RATE`, default 30/min), abuse reports per IP (5/min,
fixed), and a resolver guess-throttle (>60 unknown-code 404s per minute
from one IP → 429 for a 1-minute cooldown). **Limiter state is per replica
and resets on pod restart** — with N server replicas a client effectively
gets N× the limit. That is an accepted v1 trade-off (the limits are abuse
defenses, not billing meters); exact global limiting via a shared store is
on the roadmap. The per-IP limiters key on the first public
`X-Forwarded-For` hop, so the ingress must set/append it (standard for
nginx-class ingresses).

### 6.5 Reserved aliases

Short codes share the root path of the short domain, so a curated blacklist
prevents three squatting classes (the built-in list, ~120 words, is in
`backend/services/reserved.go`):

1. **Auth phishing on your domain** — `login`, `signin`, `password`,
   `verify`, `account` … as a short code would give an attacker a
   credential page at `https://<your-short-domain>/login`.
2. **Infra-path squatting** — `api`, `assets`, `webhook`, `metrics`,
   `www` … collide with real (or future) service routes; routing is
   path-based on every host, so this category binds on custom domains too.
3. **Legal/brand pages** — `terms`, `privacy`, `pricing`, `abuse`,
   `status`, `docs` … pages a domain is expected to serve itself; a
   squatted `/terms` misrepresents the deployment.

`RESERVED_ALIASES` (comma-separated) adds deployment-specific words —
your product names, campaign words, ingress paths; additions apply on the
shared domain *and* on custom domains. Orgs with a **verified custom
domain** are exempt from categories 1 and 3 on the grounds that they own
their namespace; category 2 and your additions always apply. Generated
codes are checked too — a random draw that spells a reserved word is
redrawn.

## 7. First boot

On startup the server:

1. Refuses to start if `JWT_SIGNING_KEY` or `SHORT_DOMAIN` is missing.
2. Runs Gofr migrations against `DB_NAME` — first boot creates the full
   schema (one initial migration). No manual schema step, but the DB and DB
   user must already exist.
3. Loads GeoIP files if configured (degrades with a logged error otherwise).
4. Serves HTTP on 8000 and metrics on 2121; readiness goes green once MySQL
   answers.

There is no seed data: the first user signs in with Google and creates the
first org through the app (multi-tenant from the first request; orgs are
self-serve). Verify a fresh deployment with
[SMOKE_TESTS.md](SMOKE_TESTS.md).
