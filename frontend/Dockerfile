# ogtr dashboard SPA (frontend) — built static files served by a tiny
# Go SPA-fallback server (no nginx, no SSR). A generic static-file image cannot
# fall back to index.html for client-side routes or set Cache-Control, so the
# runtime is server/main.go on distroless — the established opengittr pattern.
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

# SPA fallback server — index.html for unknown paths, no-cache on index.html,
# immutable on hashed /assets/*.
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS spaserver
ARG TARGETARCH
WORKDIR /src
COPY server/main.go .
RUN go mod init spaserver && CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} go build -o /spa-server .

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /srv
COPY --from=spaserver /spa-server /spa-server
COPY --from=build /build/dist/ ./static/
EXPOSE 8000
USER nonroot
ENTRYPOINT ["/spa-server"]
