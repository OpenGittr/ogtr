# ogtr — self-hosted URL shortener (all-in-one Docker image)

**Self-hosted, open-source URL shortener / link shortener** with analytics, QR
codes, custom domains, UTM tracking, and mobile deep links — deployed with
**one command**. This image bundles the whole product (dashboard UI + redirect
service + REST API + database) into a **single container on a single port**, so
you can run it with no `docker-compose` and no external database.

> Looking for a simple, private link shortener you fully control? This is a
> complete Bitly / TinyURL alternative you host yourself.

## Run ogtr in one command

```bash
docker run -p 8080:8080 -v ogtr-data:/data opengittr/ogtr
```

Then open:

| What          | URL                              |
| ------------- | -------------------------------- |
| Dashboard     | http://localhost:8080/app        |
| Short links   | http://localhost:8080/{code}     |

On first run, sign in with the built-in **dev provider** (zero setup — enter any
name + email), create an organization, and shorten your first link. The short
link at `http://localhost:8080/{code}` redirects to your destination. All data
persists in the `ogtr-data` volume across restarts.

## What's inside (one container)

- **Dashboard SPA** (React) served under `/app`.
- **Redirect + REST API backend** (Go / Gofr) on an internal port, serving the
  short-link redirects (`/{code}`), the API (`/api/*`), and health probes.
- **Embedded MariaDB** — MySQL-compatible, initialized on first run, data under
  `/data`. No external database required.
- **Caddy** reverse proxy binding the single public port `:8080` and routing the
  dashboard vs. redirects vs. API.

Routing scheme: the dashboard lives at `/app/*` so it never collides with the
short-code namespace — every other path (`/`, `/{code}`, `/api/*`,
`/.well-known/*`) is the backend.

## Configuration (override with `-e`)

Everything works out of the box; override any of these for a real deployment:

| Env var           | Default            | Purpose                                                 |
| ----------------- | ------------------ | ------------------------------------------------------- |
| `SHORT_DOMAIN`    | `localhost:8080`   | Public host used to build short URLs                    |
| `SHORT_SCHEME`    | `http`             | `https` in production                                   |
| `AUTH_PROVIDERS`  | `dev`              | `google` / `microsoft` / `dev` (comma-separated)        |
| `GOOGLE_CLIENT_ID`| —                  | Google OAuth client ID (when `google` is enabled)       |
| `MICROSOFT_CLIENT_ID` | —              | Azure app (client) ID (when `microsoft` is enabled)     |
| `JWT_SIGNING_KEY` | auto (persisted)   | Session signing key; auto-generated + stored in `/data` |
| `DB_HOST`         | `127.0.0.1`        | Set to an external host to use your own MySQL/MariaDB    |
| `DB_PORT` `DB_USER` `DB_PASSWORD` `DB_NAME` | embedded | External DB credentials                     |
| `WEBSITE_URL`     | —                  | Where the bare domain (`GET /`) bounces to              |
| `ABUSE_CONTACT`   | —                  | Contact shown on abuse/disabled-link pages              |
| `GEOIP_DB_PATH` `GEOIP_CITIES_CSV` | —   | Optional MaxMind GeoLite2 files for geo analytics       |

### A real self-host (example)

```bash
docker run -d --name ogtr -p 8080:8080 -v ogtr-data:/data \
  -e SHORT_DOMAIN=links.example.com \
  -e SHORT_SCHEME=https \
  -e AUTH_PROVIDERS=google \
  -e GOOGLE_CLIENT_ID=xxxxxxxx.apps.googleusercontent.com \
  opengittr/ogtr
```

Put your own TLS-terminating proxy (or a tunnel) in front of `:8080` and point
`links.example.com` at it. To use an external database instead of the embedded
MariaDB, set `DB_HOST`/`DB_PORT`/`DB_USER`/`DB_PASSWORD`/`DB_NAME`; the embedded
server is then skipped automatically.

> **Security note:** the default `AUTH_PROVIDERS=dev` trusts any submitted
> identity — great for evaluation, but switch to `google`/`microsoft` before
> exposing ogtr publicly.

## Persistence

`/data` holds the MariaDB data directory and the generated signing key. Mount a
named volume (or host path) there and your links, analytics, and sessions
survive `docker restart` / re-creation.

## Advanced deployments

This all-in-one image is the easy path. For horizontally-scaled or
production-grade setups, the repo also ships split images
(`backend/server`, `backend/admin`, `frontend`) and Kubernetes manifests under
`k8s/`. See the repository for details.

Image: **`opengittr/ogtr`** on Docker Hub.
