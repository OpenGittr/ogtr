# ogtr

**The link smartener — shorten, target, measure.** An open-source, self-deployable link
shortener with click analytics, QR codes, UTM campaign tracking, conditional (device/location)
redirects, and mobile deep links. Multi-tenant by design, so a single deployment can serve many
organizations.

Licensed under **MIT** (see [LICENSE](LICENSE)). The hosted service lives at
**[ogtr.in](https://ogtr.in)**; this repo is the complete self-hostable product.

## What's inside

- **`backend/server`** — the public Gofr (Go) service, MySQL: Google sign-in with
  ogtr-issued JWTs, self-serve orgs (invites, auto-join domain, org switching),
  shorten/dedupe/custom aliases, `GET /{code}` 302 redirects with click recording, QR codes,
  mobile deep links, device/city target rules (optional GeoIP), per-org custom domains with
  DNS verification, full per-link analytics + org UTM analysis, and developer API keys —
  every endpoint unit-tested, with regression tests covering the design invariants in
  [docs/FEATURES.md](docs/FEATURES.md).
- **`backend/admin`** — the separate instance-admin service (deployed as `ogtr-internal`,
  **cluster-internal only** — never on a public ingress): cross-org user/org listings,
  abuse-report triage, link disable/enable, instance-wide daily stats, behind an
  `X-Admin-Token` gate. The public server serves none of these routes.
- **Built-in abuse protection** — layered destination scanning on every create/edit
  (structural guards + optional blocklist feeds + optional Google Web Risk), periodic re-scan
  that disables now-malicious links (410 warning page), a `+`-suffix link preview page,
  public abuse reporting, creation/report/guess rate limits, and a curated reserved-alias
  list — see [docs/FEATURES.md §10](docs/FEATURES.md#10-abuse-protection).
- **`frontend`** — the dashboard SPA (React + Tailwind, CSR): all of the above with a
  responsive/mobile pass.
- **`k8s/` + Dockerfiles** — linux/amd64 images for all components and complete
  Kubernetes manifests (deployments — including the cluster-internal `ogtr-internal` —
  services, ingress for the short and app domains, configmap, secrets template).

## Quickstart

Local run (MySQL via docker-compose, backend on :5810, app on :5800) —
**no Google account setup needed to try it**: enable the built-in dev sign-in provider
(`AUTH_PROVIDERS=dev`) and log in with any name/email. See the "Zero-setup first run"
section of **[docs/LOCAL_DEVELOPMENT.md](docs/LOCAL_DEVELOPMENT.md)**. Deploying to
Kubernetes: **[docs/DEPLOYMENT.md](docs/DEPLOYMENT.md)**.

## Security-relevant configuration

A stock deployment is safe by default (structural URL guards, rate limits and reserved
aliases are always on), but a handful of knobs deserve a deliberate decision — each line
names the risk of ignoring it. Full reference: [docs/DEPLOYMENT.md](docs/DEPLOYMENT.md) §6.

| Config | What it does |
|---|---|
| `ABUSE_CONTACT` | An email (e.g. `abuse@yourdomain.com`) displayed wherever a link is blocked or disabled — creation errors, the 410 disabled-link page, and `+` previews — giving victims and falsely-flagged users a way to reach you. |
| `BLOCKLIST_FEED_URLS` | Comma-separated URLs of plaintext blocklist feeds, checked before every shorten/edit and refreshed on a schedule. This is the layer that rejects *known* phishing, malware, and crypto-mining destinations; recommended feeds (URLhaus, OpenPhish, CoinBlocker) are listed in DEPLOYMENT.md. |
| `WEBRISK_API_KEY` | Enables a real-time Google Web Risk lookup on every scan, catching fresh threats that haven't reached the public feeds yet. Optional and fail-open: a Web Risk outage never blocks link creation. |
| `RESERVED_ALIASES` | Extra words to reserve on top of the built-in ~120 (auth, infra, and legal terms): add your brand and product names and any path your short domain serves, so no user can claim e.g. `yourdomain.com/login` as their short code. |
| `LINK_CREATE_RATE` | Per-user link creations and edits per minute (default 30) — sized for humans and honest scripts. It's the brake on a stolen account or leaked API key; raise it deliberately for bulk API workloads rather than leaving it high by default. |
| `ADMIN_API_TOKEN` | Enables the instance admin API (`/api/internal/*`) on the **separate, cluster-internal `backend/admin` service** (`ogtr-internal`) — the public server never serves these routes. Cross-org user/org listings, abuse-report triage, link disable/enable, daily stats, driven by an in-cluster ops UI or curl (`X-Admin-Token` header). Unset (the default) the API answers 404 everywhere; network isolation is the primary control and the token is defense in depth. Any holder of this token has full cross-organization admin control — treat it like a root password. |

Vulnerability reports: see [SECURITY.md](SECURITY.md).

## Docs map

| Doc | What |
|---|---|
| [docs/FEATURES.md](docs/FEATURES.md) | product spec — features, design invariants, roadmap |
| [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md) | settled decisions, components, data model, API design, auth-provider boundary (with per-phase implementation notes) |
| [docs/LOCAL_DEVELOPMENT.md](docs/LOCAL_DEVELOPMENT.md) | ports, prerequisites, full local run, OAuth + GeoIP setup, tests/lint |
| [docs/DEPLOYMENT.md](docs/DEPLOYMENT.md) | image builds, k8s walkthrough, config/secret reference, GeoIP volume, domains/TLS |
| [docs/SMOKE_TESTS.md](docs/SMOKE_TESTS.md) | manual smoke checklist for a fresh deployment |
| [SECURITY.md](SECURITY.md) | vulnerability disclosure policy |
| [k8s/README.md](k8s/README.md) | manifest inventory and apply order |

## Stack

- **Backend** — Go with the Gofr framework, MySQL, framework-managed migrations (run at boot).
- **Frontend** — React + Tailwind CSS (CSR, static-served; no SSR, no nginx).
- **Deployment** — Docker images on Kubernetes; all manifests under `k8s/`.

## Repository layout

```
docs/            high-level docs (FEATURES, ARCHITECTURE, DEPLOYMENT, ...)
backend/         one Go module; shared packages (handlers, services, stores,
                 models, migrations, ...) plus two service entrypoints:
backend/server/  the public product (redirects, /api/v1, migrations at boot)
backend/admin/   the instance-admin service (/api/internal/*; deployed as
                 the cluster-internal `ogtr-internal` — dir is `admin` only
                 because `internal/` is a reserved Go directory name)
frontend/        dashboard SPA (React + Tailwind)
k8s/             all deployment manifests
```
