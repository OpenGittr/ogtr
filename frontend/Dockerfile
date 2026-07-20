# ogtr dashboard SPA (frontend) — built static files served by gostatic
# (opengittr/gostatic): SPA fallback, no-cache HTML + immutable
# hashed assets by default, no nginx, no SSR.
#
# Build for linux/amd64 (deployment target; we develop on Mac):
#   docker build --platform linux/amd64 \
#     --build-arg VITE_API_URL=https://links.example.com \
#     --build-arg VITE_GOOGLE_CLIENT_ID=<client-id>.apps.googleusercontent.com \
#     -t ogtr-app frontend
#
# VITE_* values are baked into the bundle at BUILD time (both are publishable,
# not secrets). VITE_API_URL must be the backend origin (SHORT_DOMAIN with
# scheme); empty means "same origin", which only works behind a dev proxy.
ARG VITE_API_URL=""
ARG VITE_GOOGLE_CLIENT_ID=""

# Build stage runs natively (BUILDPLATFORM) — emitted static files are
# arch-independent; only the final stage is the target platform.
FROM --platform=$BUILDPLATFORM node:24-alpine AS build
ARG VITE_API_URL
ARG VITE_GOOGLE_CLIENT_ID
WORKDIR /build
COPY package.json package-lock.json ./
RUN npm ci
COPY . .
ENV VITE_API_URL=${VITE_API_URL} \
    VITE_GOOGLE_CLIENT_ID=${VITE_GOOGLE_CLIENT_ID}
RUN npm run build

# gostatic base image carries the server, ports (8000 http / 9090 metrics),
# and entrypoint; it serves /srv/static by default.
FROM opengittr/gostatic:v0.1.0
COPY --from=build --chown=nonroot:nonroot /build/dist /srv/static
