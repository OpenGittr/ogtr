# k8s — deployment manifests

All Kubernetes manifests for ogtr. One deployment file per component
(`server`, `internal`, `app`), plus namespace, configmap, secrets template
and ingress. Full walkthrough: [docs/DEPLOYMENT.md](../docs/DEPLOYMENT.md).

| File | What |
|---|---|
| `namespace.yaml` | `ogtr` namespace |
| `configmap.yaml` | non-secret backend config (ports, domains, TTLs, GeoIP paths) |
| `secrets-template.yaml` | **template** — DB creds, JWT signing key, Google client ID |
| `server-deployment.yaml` | public server Deployment + Service (probes, metrics annotations, optional GeoIP volume) |
| `internal-deployment.yaml` | **cluster-internal** instance-admin service (`ogtr-internal`, from `backend/admin`) — NEVER exposed on any ingress |
| `app-deployment.yaml` | dashboard SPA Deployment + Service |
| `ingress.yaml` | one Ingress, two hosts (short / app domains); `ogtr-internal` is deliberately absent |

## Before applying

1. **Domains** — replace the `links.example.com` / `app.example.com`
   placeholders in `ingress.yaml` and `configmap.yaml` with your domains; bake
   the matching `VITE_API_URL` (+ `VITE_GOOGLE_CLIENT_ID`) into the app image.
2. **Images** — no registry is decided yet; build the three images
   (`backend/server/Dockerfile` and `backend/admin/Dockerfile`, both with
   build context `backend/`, plus the `frontend` Dockerfile —
   `--platform linux/amd64`), push to your registry, and prefix the `image:`
   fields in the deployments.
3. **Secrets** — copy `secrets-template.yaml` outside the repo, fill in real
   values, and keep that copy somewhere private outside the repo. Never commit
   real values.
4. **MySQL** — provision any MySQL 8+ reachable from the cluster and point
   `db-host`/`db-user`/`db-password` at it. The server runs its migrations at
   boot, so the user needs DDL rights on the `ogtr` database.

## Apply order

```sh
kubectl apply -f k8s/namespace.yaml
kubectl apply -f /path/outside/the/repo/secrets.yaml   # your filled-in copy
kubectl apply -f k8s/configmap.yaml
kubectl apply -f k8s/server-deployment.yaml
kubectl apply -f k8s/internal-deployment.yaml   # optional — only if you use the admin API
kubectl apply -f k8s/app-deployment.yaml
kubectl apply -f k8s/ingress.yaml
```

**`ogtr-internal` is cluster-internal only.** Its ClusterIP service
(`http://ogtr-internal.ogtr.svc.cluster.local`) is the only way to reach the
instance-admin API — never add it to an ingress. Network isolation is the
primary control; the `admin-api-token` secret key (`ADMIN_API_TOKEN`) is
defense in depth. Without the token key the service runs dark (every
`/api/internal/*` request answers 404).

Probes use Gofr's built-in health endpoints (`/.well-known/alive` liveness,
`/.well-known/health` readiness — the latter checks MySQL connectivity, so
the server only receives traffic once the DB is reachable). Prometheus scrape
annotations point at the dedicated metrics port (2121).

## GeoIP (optional)

Location targeting + click geo need the MaxMind GeoLite2-City `.mmdb` and
locations CSV, which are licensed and never baked into the image. Mount them
at `/data/geoip/` via the commented-out volume in `server-deployment.yaml`
and set `GEOIP_DB_PATH` / `GEOIP_CITIES_CSV` in `configmap.yaml`. Without
them everything else works (clicks record no city, location rules never
match, city autocomplete returns 501). See docs/DEPLOYMENT.md for the
step-by-step, including keeping both files from the same GeoLite2 release.
