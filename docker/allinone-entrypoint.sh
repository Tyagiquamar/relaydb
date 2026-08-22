#!/bin/sh
# All-in-one demo entrypoint: an in-container PostgreSQL 16 (logical
# replication enabled) plus api + capture + delivery in one container for
# free-tier hosts (Render/SnapDeploy). The commerce source data is disposable
# demo state, so no external database service is required.
set -eu

export PGDATA="${PGDATA:-/var/lib/postgresql/data}"
export PGUSER="${PGUSER:-relaydb}"
export PGPASSWORD="${PGPASSWORD:-relaydb}"

mkdir -p "$PGDATA" /run/postgresql
chown -R postgres:postgres "$PGDATA" /run/postgresql
chmod 700 "$PGDATA"

# --- PostgreSQL bootstrap ---------------------------------------------------
if [ ! -s "$PGDATA/PG_VERSION" ]; then
  su-exec postgres initdb -D "$PGDATA" --auth=trust >/dev/null
fi

su-exec postgres postgres -D "$PGDATA" \
  -c config_file=/etc/postgresql/postgresql.conf \
  -c hba_file=/etc/postgresql/pg_hba.conf \
  -c listen_addresses=localhost \
  -c port=5433 &

until su-exec postgres pg_isready -q -h localhost -p 5433; do sleep 0.2; done

# Role and databases (idempotent).
su-exec postgres psql -h localhost -p 5433 -U postgres -v ON_ERROR_STOP=1 <<'SQL'
DO $$
BEGIN
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'relaydb') THEN
    CREATE ROLE relaydb LOGIN SUPERUSER REPLICATION PASSWORD 'relaydb';
  END IF;
END
$$;
SQL

su-exec postgres psql -h localhost -p 5433 -U postgres -tAc \
  "SELECT 1 FROM pg_database WHERE datname='commerce'" | grep -q 1 \
  || su-exec postgres createdb -h localhost -p 5433 -U postgres -O relaydb commerce

su-exec postgres psql -h localhost -p 5433 -U postgres -tAc \
  "SELECT 1 FROM pg_database WHERE datname='relaydb'" | grep -q 1 \
  || su-exec postgres createdb -h localhost -p 5433 -U postgres -O relaydb relaydb

# Seed the commerce source once (creates tables + relaydb_pub publication).
if ! su-exec postgres psql -h localhost -p 5433 -U postgres -d commerce -tAc \
  "SELECT 1 FROM pg_publication WHERE pubname='relaydb_pub'" | grep -q 1; then
  su-exec postgres psql -h localhost -p 5433 -U postgres -d commerce -v ON_ERROR_STOP=1 \
    -f /seed/commerce-seed.sql
fi

# --- RelayDB services ---------------------------------------------------------
# The api binary applies embedded metadata migrations on startup.
export RELAYDB_METADATA_DB_URL="${RELAYDB_METADATA_DB_URL:-postgres://relaydb:relaydb@localhost:5433/relaydb?sslmode=disable}"
export RELAYDB_SOURCE_DB_URL="${RELAYDB_SOURCE_DB_URL:-postgres://relaydb:relaydb@localhost:5433/commerce?replication=database}"
# App listens on the port the platform edge routes to (5432); Postgres moved to 5433.
export RELAYDB_HTTP_ADDR="${RELAYDB_HTTP_ADDR:-:5432}"
export RELAYDB_GRPC_ADDR="${RELAYDB_GRPC_ADDR:-:9090}"
export RELAYDB_ADMIN_KEY_ID="${RELAYDB_ADMIN_KEY_ID:-admin}"
export RELAYDB_ADMIN_KEY="${RELAYDB_ADMIN_KEY:?RELAYDB_ADMIN_KEY is required}"
export RELAYDB_READER_KEY_ID="${RELAYDB_READER_KEY_ID:-reader}"
export RELAYDB_READER_KEY="${RELAYDB_READER_KEY:?RELAYDB_READER_KEY is required}"
export RELAYDB_MASTER_KEY="${RELAYDB_MASTER_KEY:-MDEyMzQ1Njc4OTAxMjM0NTY3ODkwMTIzNDU2Nzg5MDE=}"

# Start api first and let it apply embedded migrations before the others
# connect (avoids a CREATE TABLE race on schema_migrations when all three
# processes start at once against a fresh database).
RELAYDB_SERVICE=api /bin/api &
API_PID=$!
sleep 3
RELAYDB_SERVICE=capture RELAYDB_CAPTURE_OWNER_ID=capture-1 RELAYDB_METRICS_ADDR=:2112 /bin/capture &
RELAYDB_SERVICE=delivery RELAYDB_METRICS_ADDR=:2113 /bin/delivery &

# Self-driving commerce traffic: real order lifecycles written into the source
# schema on an interval so capture -> event store -> webhooks always carry
# fresh data. Disable with DEMO_TRAFFIC=false.
if [ "${DEMO_TRAFFIC:-true}" = "true" ]; then
  RELAYDB_SERVICE=demo-commerce RELAYDB_HTTP_ADDR=:8081 \
    DEMO_TRAFFIC_INTERVAL_SECS="${DEMO_TRAFFIC_INTERVAL_SECS:-45}" /bin/demo-commerce &
fi

# Exit (and let the platform restart the container) when any component dies.
wait -n
echo "allinone: a component exited, shutting down for restart" >&2
exit 1
