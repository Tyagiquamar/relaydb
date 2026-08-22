# RelayDB Deployment

## Hosted demo

The public demo is a single self-contained container: PostgreSQL 16 with
logical replication enabled plus the API, capture, delivery, and demo-commerce
services, hosted on SnapDeploy's free tier. The operations dashboard runs on
Vercel and talks to it through a server-side BFF proxy.

| Component | Runtime | Public URL |
|---|---|---|
| RelayDB stack (source PostgreSQL, metadata store, api, capture, delivery, commerce traffic) | SnapDeploy free tier, built from `Dockerfile.demo` | https://relaydb.containers.snapdeploy.app |
| Operations dashboard | Vercel | https://relaydb-dashboard.vercel.app |

Verified 2026-08-22: `GET https://relaydb.containers.snapdeploy.app/health/live`
returns `200`.

### How the single container works

`Dockerfile.demo` builds the Go binaries into a `postgres:16-alpine` base.
`docker/allinone-entrypoint.sh` initializes an in-container PostgreSQL with
logical replication configured, creates and seeds the commerce schema and the
`relaydb_pub` publication, then starts the API (which applies embedded
migrations first), capture, delivery, and a commerce traffic generator that
posts real order lifecycles every 45 seconds so capture → event store →
webhook delivery always carry fresh data.

Two constraints shaped it:

- The app listens on port 5432 (`RELAYDB_HTTP_ADDR=:5432`) because that is the
  port the platform edge routes to; PostgreSQL moved internally to 5433.
- Demo state is disposable. The free tier provides no persistent disk, so the
  in-container database resets on redeploy and re-seeds itself. History does
  not survive restarts by design.

Free-tier instances sleep when idle; a cold wake shows fresh CDC data within
about a minute. The dashboard defaults to live mode and retries through the
wake window. It never substitutes fixture data when the API is unavailable or
empty — unavailable reads stay visibly unavailable.

## Required variables

Generate independent production secrets. Do not reuse Compose development
defaults outside local development.

| Surface | Variables |
|---|---|
| Container host service | `RELAYDB_ADMIN_KEY`, `RELAYDB_READER_KEY`; optional `DEMO_TRAFFIC=false`, `DEMO_TRAFFIC_INTERVAL_SECS` |
| Vercel dashboard | `RELAYDB_API_URL=https://relaydb.containers.snapdeploy.app`, `RELAYDB_READER_KEY_ID`, `RELAYDB_READER_KEY` (all server-side only; the browser only calls same-origin `/api/v1/*`) |

Connection strings (`RELAYDB_METADATA_DB_URL`,
`RELAYDB_SOURCE_DB_URL`), addresses, key IDs, and `RELAYDB_MASTER_KEY` have
sane single-container defaults baked into the entrypoint. The platform-assigned
port is honored automatically.

## Reproducing the deploy

1. In SnapDeploy (or any container host): create a service from this
   repository, Dockerfile path `./Dockerfile.demo`, health check path
   `/health/live`. Set `RELAYDB_ADMIN_KEY` and `RELAYDB_READER_KEY` before the
   first deploy.
2. Confirm `GET https://<service-domain>/health/live` responds with `200`.
3. On Vercel: import `dashboard/`, set the three dashboard variables as
   server-side environment variables, deploy, and confirm events appear on the
   live dashboard within a minute of wake-up.

## Multi-service layout

For a production-shaped split (separate source PostgreSQL with a persistent
volume, metadata PostgreSQL, and individual api/capture/delivery/
demo-commerce containers), use `docker-compose.yml` locally, or the root
`Dockerfile` on any container host: set `RELAYDB_RUN` to one of `api`,
`capture`, `delivery`, or `demo-commerce` per service and attach a volume at
`/var/lib/postgresql/data` on the source database before its first deploy. The
source image builds from `docker/postgres` and initializes the commerce schema
plus the `relaydb_pub` publication. Per-service variables are listed in the
Compose file.

## Verified local stack

The complete dashboard read path has been smoke-tested with Docker Compose:

```powershell
docker compose up -d source-postgres metadata-postgres api dashboard
curl http://localhost:3000/api/v1/stats
curl "http://localhost:3000/api/v1/events?limit=1"
```

The dashboard runs at `http://localhost:3000`. Its `/api/v1/*` route is a
server-side BFF proxy: the reader key remains in the dashboard runtime and is
not exposed to the browser.

To create fresh CDC traffic, start the demo service and post an order:

```powershell
docker compose --profile demo up -d demo-commerce
curl.exe -X POST http://localhost:8081/orders -H "Content-Type: application/json" -d '{"customer_id":1,"items":[{"product_id":1,"quantity":1}]}'
```
