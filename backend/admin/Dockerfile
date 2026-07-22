# ogtr instance-admin service (deployed as `ogtr-internal`) — the operator
# API under /api/internal/*, split out of the public server so cross-org
# admin surface never shares a listener with internet-facing traffic.
# CLUSTER-INTERNAL ONLY: never expose this image on any public ingress.
#
# Build context is backend/ (the module root; this Dockerfile builds the
# ./admin entrypoint). For linux/amd64:
#   docker build --platform linux/amd64 -f backend/admin/Dockerfile -t ogtr-internal backend
#
# No migrations in this image — the public server owns the schema and
# migrates at boot; this service only queries the same database.

FROM --platform=$BUILDPLATFORM golang:1.26 AS build
ARG TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} go build -buildvcs=false -o /admin ./admin

# Distroless static, non-root (uid 65532). No shell, no package manager.
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /admin /app/admin
# Sample config only, for reference — admin/configs/.local.env is
# dockerignored and real configuration always comes from the pod environment.
COPY admin/configs/.sample.env /app/configs/.sample.env
# Gofr defaults: HTTP on 8000, Prometheus metrics on 2121 (set explicitly in
# the k8s manifest; local dev uses 5820/5821 via admin/configs/.local.env).
EXPOSE 8000 2121
USER nonroot
ENTRYPOINT ["/app/admin"]
