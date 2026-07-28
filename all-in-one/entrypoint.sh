#!/usr/bin/env bash
# ogtr all-in-one entrypoint: boots embedded MariaDB, the ogtr backend, and a
# Caddy reverse proxy in ONE container, reachable on ONE port (:8080).
#
# All state lives under /data (mount a volume there to persist across
# restarts). Every config value below is overridable with `docker run -e ...`
# so a real self-host can point at an external MySQL/MariaDB, set a real
# domain, enable Google/Microsoft auth, etc.
set -euo pipefail

DATA_DIR=/data
DB_DATA="${DATA_DIR}/mysql"
SOCKET=/run/mysqld/mysqld.sock

log() { echo "[ogtr-aio] $*"; }

# --- config defaults (all overridable with -e) -------------------------------
export DB_DIALECT="${DB_DIALECT:-mysql}"
export DB_HOST="${DB_HOST:-127.0.0.1}"
export DB_PORT="${DB_PORT:-3306}"
export DB_USER="${DB_USER:-ogtr}"
export DB_PASSWORD="${DB_PASSWORD:-ogtr}"
export DB_NAME="${DB_NAME:-ogtr}"

export HTTP_PORT="${HTTP_PORT:-8000}"
export METRICS_PORT="${METRICS_PORT:-2121}"
export APP_NAME="${APP_NAME:-ogtr}"
export LOG_LEVEL="${LOG_LEVEL:-INFO}"

# Zero-setup first run: the dev provider trusts any submitted name+email.
# Override with AUTH_PROVIDERS=google (+ GOOGLE_CLIENT_ID) for a real deploy.
export AUTH_PROVIDERS="${AUTH_PROVIDERS:-dev}"

# The public origin used to build short URLs. Defaults to the localhost
# demo host; set SHORT_DOMAIN=links.example.com SHORT_SCHEME=https in prod.
export SHORT_DOMAIN="${SHORT_DOMAIN:-localhost:8080}"
export SHORT_SCHEME="${SHORT_SCHEME:-http}"

# Embedded DB is managed only when DB_HOST points at the loopback. If the
# caller supplies an external host, skip booting the local MariaDB entirely.
EMBEDDED_DB=true
case "${DB_HOST}" in
	127.0.0.1 | localhost | ::1) EMBEDDED_DB=true ;;
	*) EMBEDDED_DB=false ;;
esac

# Persist a generated JWT signing key so sessions survive restarts, unless the
# operator supplies their own JWT_SIGNING_KEY.
if [ -z "${JWT_SIGNING_KEY:-}" ]; then
	key_file="${DATA_DIR}/jwt_signing_key"
	if [ ! -f "${key_file}" ]; then
		mkdir -p "${DATA_DIR}"
		head -c 48 /dev/urandom | base64 | tr -d '\n' >"${key_file}"
	fi
	JWT_SIGNING_KEY="$(cat "${key_file}")"
	export JWT_SIGNING_KEY
fi

MARIADB_PID=""

shutdown() {
	log "shutting down..."
	if [ -n "${MARIADB_PID}" ]; then
		mariadb-admin --socket="${SOCKET}" -u root shutdown 2>/dev/null || true
	fi
	# Signal the remaining process group members (backend, caddy).
	kill 0 2>/dev/null || true
	wait 2>/dev/null || true
}
trap shutdown TERM INT

if [ "${EMBEDDED_DB}" = "true" ]; then
	mkdir -p "${DB_DATA}" /run/mysqld
	chown -R mysql:mysql "${DB_DATA}" /run/mysqld

	if [ ! -d "${DB_DATA}/mysql" ]; then
		log "initializing embedded MariaDB data directory at ${DB_DATA}"
		mariadb-install-db --user=mysql --datadir="${DB_DATA}" \
			--auth-root-authentication-method=normal --skip-test-db >/dev/null
	fi

	log "starting MariaDB"
	gosu mysql mariadbd --datadir="${DB_DATA}" --socket="${SOCKET}" \
		--bind-address=127.0.0.1 --port="${DB_PORT}" \
		--pid-file=/run/mysqld/mysqld.pid &
	MARIADB_PID=$!

	log "waiting for MariaDB to accept connections"
	ready=false
	for _ in $(seq 1 60); do
		if mariadb-admin --socket="${SOCKET}" -u root ping >/dev/null 2>&1; then
			ready=true
			break
		fi
		sleep 1
	done
	if [ "${ready}" != "true" ]; then
		log "MariaDB did not become ready in time"
		exit 1
	fi

	log "ensuring database '${DB_NAME}' and user '${DB_USER}'"
	mariadb --socket="${SOCKET}" -u root <<-SQL
		CREATE DATABASE IF NOT EXISTS \`${DB_NAME}\` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci;
		CREATE USER IF NOT EXISTS '${DB_USER}'@'%' IDENTIFIED BY '${DB_PASSWORD}';
		CREATE USER IF NOT EXISTS '${DB_USER}'@'localhost' IDENTIFIED BY '${DB_PASSWORD}';
		GRANT ALL PRIVILEGES ON \`${DB_NAME}\`.* TO '${DB_USER}'@'%';
		GRANT ALL PRIVILEGES ON \`${DB_NAME}\`.* TO '${DB_USER}'@'localhost';
		FLUSH PRIVILEGES;
	SQL
else
	log "external database configured (DB_HOST=${DB_HOST}); skipping embedded MariaDB"
fi

log "starting ogtr backend on 127.0.0.1:${HTTP_PORT} (runs migrations)"
(cd /app && exec ./server) &

log "starting Caddy reverse proxy on :8080"
export XDG_CONFIG_HOME=/tmp XDG_DATA_HOME=/tmp
caddy run --config /etc/caddy/Caddyfile --adapter caddyfile &

log "ogtr is up — dashboard http://localhost:8080/app  |  short links http://localhost:8080/{code}"

# If any managed process exits, tear the rest down so the container stops
# (and an orchestrator's restart policy can recreate it cleanly).
wait -n
log "a managed process exited; shutting down"
shutdown
exit 1
