# Local development

How to run all of ogtr on one machine. Project decisions and conventions
live in [ARCHITECTURE.md](ARCHITECTURE.md); the feature spec in
[FEATURES.md](FEATURES.md).

## Zero-setup first run (no Google account needed)

The fastest way to try ogtr — no Google OAuth client, no cloud console.
The **dev sign-in provider** (`AUTH_PROVIDERS=dev`) lets you sign in with any
name and email; everything after sign-in (orgs, links, analytics) is the real
product. It performs no credential check, so it is for local evaluation only —
never enable it in production.

```sh
# 1. MySQL
docker compose up -d

# 2. Minimal backend config
cat > backend/server/configs/.local.env <<'EOF'
DB_PASSWORD=ogtr-local
JWT_SIGNING_KEY=any-long-random-string-for-local-use
AUTH_PROVIDERS=dev
EOF

# 3. Backend (listens on :5810, migrates on boot)
cd backend/server && go run .

# 4. Dashboard SPA — open http://localhost:5800 in another terminal
cd frontend && npm install && npm run dev
```

Open http://localhost:5800, fill in the amber "Development mode sign-in" form
with any name/email, create an org, shorten a link — done. Set
`AUTH_PROVIDERS=google,dev` (plus `GOOGLE_CLIENT_ID`, see "Google OAuth"
below) to test both sign-in methods side by side; the backend logs a WARN at
boot whenever dev is enabled.

## Ports

The project defaults to the **5800–5899** port block so several local stacks
can coexist. Everything fails loudly on conflict (`strictPort: true` in Vite,
explicit ports in Gofr config).

| Port | Service |
|---|---|
| 5800 | frontend — Vite dev server (dashboard SPA) |
| 5810 | backend/server (public server) — HTTP |
| 5811 | backend/server — Prometheus metrics |
| 5820 | backend/admin (instance-admin service) — HTTP |
| 5821 | backend/admin — Prometheus metrics |
| 5830 | MySQL (docker-compose host port) |

## Prerequisites

- **Go 1.26+** (gofr v1.57 requires it).
- **Node 24+** and npm.
- **Docker** (colima on Mac is fine) for the MySQL container.
- **golangci-lint built with go1.26+** — a golangci-lint binary compiled with
  an older Go toolchain cannot lint this module. If needed, build one with the
  1.26 toolchain:
  `go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest`.

## First-time setup

```sh
# 1. MySQL (host port 5830, credentials in docker-compose.yml)
docker compose up -d

# 2. Backend config — copy the sample and fill in the blanks
cp backend/server/configs/.sample.env backend/server/configs/.local.env
# Set at least: DB_PASSWORD=ogtr-local, JWT_SIGNING_KEY=<any long random
# string>, and either AUTH_PROVIDERS=dev (zero-setup, see the section above)
# or GOOGLE_CLIENT_ID=<see "Google OAuth" below>.
# .local.env is gitignored — real values never enter git.

# 3. Backend (runs migrations on boot, listens on 5810/5811)
cd backend/server && go run .

# 4. Dashboard SPA — http://localhost:5800
cd frontend && npm install && npm run dev
```

## Instance-admin service (optional, second terminal)

The operator API (`/api/internal/*`, ARCHITECTURE.md "Instance admin
service") is a separate service in `backend/admin` — the public server on
5810 never serves it. Run it only when you're working on the admin surface
(e.g. driving it from the operations console):

```sh
cp backend/admin/configs/.sample.env backend/admin/configs/.local.env
# Set DB_PASSWORD=ogtr-local and ADMIN_API_TOKEN=<openssl rand -hex 32>.

cd backend/admin && go run .   # listens on 5820/5821, no migrations

curl -H "X-Admin-Token: <your token>" http://localhost:5820/api/internal/users
```

Without `ADMIN_API_TOKEN` (or with a wrong `X-Admin-Token`) every admin
endpoint answers 404. On 5810 the same paths are just unknown API routes.

The Vite dev server on 5800 proxies `^/api/` to the backend on 5810, so the
SPA works same-origin with no CORS setup. The backend's
`ACCESS_CONTROL_ALLOW_ORIGIN` (sample: `http://localhost:5800`) covers
direct cross-origin calls.

## Google OAuth (local)

Only needed to test the real Google sign-in (for a quick evaluation use the
dev provider — see "Zero-setup first run"). Sign-in uses Google Identity
Services in the SPA; the backend verifies the ID token (audience =
`GOOGLE_CLIENT_ID`, keys from `GOOGLE_JWKS_URL`, default Google's published
set). There is no hidden auth bypass — the dev provider is an explicit,
opt-in `AUTH_PROVIDERS` entry.

1. Create an OAuth **Web application** client in Google Cloud Console.
2. Add `http://localhost:5800` to **Authorized JavaScript origins** — GIS
   refuses to issue tokens to unlisted origins.
3. Put the client ID in `backend/server/configs/.local.env`
   (`GOOGLE_CLIENT_ID=...`) and make sure `AUTH_PROVIDERS` includes `google`.
   The SPA gets the client ID from `GET /api/v1/auth/providers`; setting
   `VITE_GOOGLE_CLIENT_ID` in `frontend/.env.local` is only a fallback
   (the server-provided value takes precedence). Reuse your existing OAuth
   client ID if you already created one for this project.

Tests and E2E scripts avoid real Google entirely by pointing
`GOOGLE_JWKS_URL` at a local JWKS server and minting RS256 tokens against it;
the verification code path is identical.

## Microsoft sign-in (local)

Optional, like Google: enable it only to test the real "Sign in with
Microsoft" button. The SPA runs the OAuth authorization-code + PKCE flow
itself (no MSAL); the backend verifies the resulting ID token
(audience = `MICROSOFT_CLIENT_ID`, keys from `MICROSOFT_JWKS_URL`, default
Microsoft's published `common` v2.0 set).

1. In the Azure portal (Microsoft Entra ID → App registrations) create a new
   registration.
   - **Supported account types**: "Accounts in any organizational directory
     and personal Microsoft accounts" (work/school + personal).
   - **Platform**: add a **Single-page application** platform (this is what
     enables the CORS token exchange PKCE needs — *not* "Web").
   - **Redirect URIs** (on the SPA platform): `http://localhost:5800/auth/microsoft`
     for local dev, plus `https://<your-app-origin>/auth/microsoft` for each
     deployed dashboard origin.
2. Put the registration's **Application (client) ID** in
   `backend/server/configs/.local.env` (`MICROSOFT_CLIENT_ID=...`) and add
   `microsoft` to `AUTH_PROVIDERS`. The SPA gets the client ID from
   `GET /api/v1/auth/providers`; there is no build-time fallback.

Tests avoid real Microsoft entirely by pointing `MICROSOFT_JWKS_URL` at a
local JWKS server and minting RS256 tokens against it; the verification code
path is identical.

## GeoIP (optional, local)

Location targeting, click geo and city autocomplete need MaxMind GeoLite2
files, which are licensed and **never committed** (`backend/server/data/` is
gitignored). Download with your own MaxMind license key and set in
`backend/server/configs/.local.env` (paths relative to the `backend/server`
working directory):

```
GEOIP_DB_PATH=data/GeoLite2-City.mmdb
GEOIP_CITIES_CSV=data/GeoLite2-City-Locations-en.csv
```

Use both files from the **same GeoLite2 release** (city names drift between
vintages). Without them everything else works: clicks record no city,
location rules never match, `GET /api/v1/cities` returns 501.

## Custom domains locally (no real DNS needed)

The custom-domain feature (FEATURES.md §1.6) routes by Host header, so
everything except the DNS TXT verification can be exercised locally with
`curl -H 'Host: ...'`:

```sh
# Register a domain in the dashboard (Domains page) or via the API, then:

# Verification will 409 locally (no real TXT record) — that failure path is
# the honest one. To test the post-verification behavior, flip the dev DB:
docker exec ogtr-mysql mysql -uroot -pogtr-local ogtr \
  -e "UPDATE domains SET status='VERIFIED', verified_at=NOW() WHERE hostname='links.example.com'"

# A verified custom domain serves ONLY its org's links:
curl -i -H 'Host: links.example.com' http://localhost:5810/<own-org-code>    # 302
curl -i -H 'Host: links.example.com' http://localhost:5810/<other-org-code>  # 404
curl -i -H 'Host: links.example.com' http://localhost:5810/                  # 404 (no website bounce)

# The deployment domain (and localhost) keeps the global namespace:
curl -i http://localhost:5810/<any-code>                                     # 302
```

Ports in the Host header are ignored (`localhost:5810` ≡ `localhost`).
To verify a domain for real locally you'd need a hostname whose
`_ogtr-verify.` TXT record you can actually publish — any domain you own
works, since verification only reads public DNS.

## Tests and lint

```sh
# Backend — unit tests (sqlmock + gomock; no DB needed)
cd backend && go test ./... -race

# Backend — lint (see prerequisites re: go1.26 golangci-lint build)
golangci-lint run ./...

# Frontend — type-check + production build
cd frontend && npm run build
```

CI (`.github/workflows/ci.yaml`) runs the same: backend build/vet/test with
race + coverage, the frontend build, and build-only checks of all three
Docker images (server, internal, app).

## Docker images (local build)

See [DEPLOYMENT.md](DEPLOYMENT.md) for the full build/deploy story. Quick
sanity build (deployment target is linux/amd64 — we develop on Mac):

```sh
docker build --platform linux/amd64 -f backend/server/Dockerfile -t ogtr-server backend
docker build --platform linux/amd64 -f backend/admin/Dockerfile -t ogtr-internal backend
docker build --platform linux/amd64 \
  --build-arg VITE_API_URL=http://localhost:5810 \
  --build-arg VITE_GOOGLE_CLIENT_ID=<client-id> \
  -t ogtr-app frontend
```

## Resetting local state

`docker compose down -v` wipes the MySQL volume; the next backend boot
re-runs the migration into the empty database. During development, schema
changes are made by editing the initial migration file in place and applying
matching `ALTER`s to the local DB (convention: one migration file per
production deployment).
