# ogtr all-in-one: dashboard UI + redirect/API backend + embedded MariaDB in
# ONE image, reachable on ONE port (8080). For self-hosters who want the whole
# product with a single `docker run` — no docker-compose, no external DB.
#
#   docker run -p 8080:8080 -v ogtr-data:/data opengittr/ogtr
#
#   Dashboard:   http://localhost:8080/app
#   Short links: http://localhost:8080/{code}
#
# This is a SEPARATE convenience artifact. Production / advanced deployments use
# the split images (backend/server, backend/admin, frontend) + k8s/ manifests,
# which this file deliberately does not touch.
#
# Build context is the REPO ROOT:
#   docker build -f all-in-one/Dockerfile -t opengittr/ogtr .

# ---- Stage 1: build the Go backend (static linux binary) --------------------
FROM --platform=$BUILDPLATFORM golang:1.26 AS backend
ARG TARGETARCH
WORKDIR /src
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} go build -buildvcs=false -o /server ./server

# ---- Stage 2: build the dashboard SPA, based at /app ------------------------
FROM --platform=$BUILDPLATFORM node:24-alpine AS frontend
WORKDIR /build
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ ./
# Same-origin API (VITE_API_URL=""); dev-login is enabled by default so no
# Google client is needed for first run. --base=/app/ mounts the SPA under
# /app so it never collides with the /{code} short-link namespace at the root
# (react-router picks up the base via import.meta.env.BASE_URL).
ENV VITE_API_URL="" \
    VITE_GOOGLE_CLIENT_ID=""
RUN npm run build -- --base=/app/

# ---- Stage 3: runtime — MariaDB + Caddy + backend + SPA ---------------------
FROM mariadb:11

# Caddy: single static binary, copied from the official image (multi-arch).
COPY --from=caddy:2 /usr/bin/caddy /usr/bin/caddy

COPY --from=backend /server /app/server
COPY --from=frontend /build/dist /srv/www/app
COPY all-in-one/Caddyfile /etc/caddy/Caddyfile
COPY all-in-one/entrypoint.sh /usr/local/bin/ogtr-entrypoint
RUN chmod +x /usr/local/bin/ogtr-entrypoint

# All persistent state (database + generated signing key) lives here.
VOLUME /data
EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/ogtr-entrypoint"]
