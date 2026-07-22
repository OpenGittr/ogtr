# ogtr public server — Gofr service (redirects at /, API under /api/v1).
# Build context is backend/ (the module root; this Dockerfile builds the
# ./server entrypoint). For linux/amd64 (deployment target; we develop on Mac):
#   docker build --platform linux/amd64 -f backend/server/Dockerfile -t ogtr-server backend
#
# Multi-stage: the build stage runs natively (BUILDPLATFORM) and cross-compiles
# a static linux binary for TARGETARCH; the runtime stage is distroless and
# runs as a non-root user. Migrations are compiled in and run at boot.
#
# The instance-admin API is NOT in this image — it is the separate
# backend/admin service (backend/admin/Dockerfile, deployed as ogtr-internal).
#
# GeoIP is NOT baked in (MaxMind-licensed data, see docs/ARCHITECTURE.md §6) —
# the app degrades gracefully without it. Deployments mount the .mmdb/CSV at
# the conventional path /data/geoip/ and set:
#   GEOIP_DB_PATH=/data/geoip/GeoLite2-City.mmdb
#   GEOIP_CITIES_CSV=/data/geoip/GeoLite2-City-Locations-en.csv

FROM --platform=$BUILDPLATFORM golang:1.26 AS build
ARG TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} go build -buildvcs=false -o /server ./server

# Distroless static, non-root (uid 65532). No shell, no package manager.
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /server /app/server
# Sample config only, for reference — server/configs/.local.env is
# dockerignored and real configuration always comes from the pod environment
# (k8s configmap + secret). Gofr falls back to process env vars when
# configs/.env is absent.
COPY server/configs/.sample.env /app/configs/.sample.env
# Gofr defaults: HTTP on 8000, Prometheus metrics on 2121 (set explicitly in
# the k8s configmap; local dev uses 5810/5811 via server/configs/.local.env).
EXPOSE 8000 2121
USER nonroot
ENTRYPOINT ["/app/server"]
