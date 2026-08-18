# RelayDB Deployment

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

## Production topology

| Service | Runtime | Purpose |
|---|---|---|
| Dashboard | Vercel | Public UI and server-side BFF proxy |
| API | Railway service | REST, gRPC, health, and metrics |
| Capture | Railway service | Logical replication ingestion |
| Delivery | Railway service | Webhook retry and DLQ loop |
| Demo commerce | Railway service | Public order API that generates demo CDC events |
| Source PostgreSQL | Railway service + volume | Logical replication source with `relaydb_pub` |
| Metadata PostgreSQL | Railway Postgres | Event store, checkpoints, and application metadata |

The root `Dockerfile` defaults to a Railway launcher. Set `RELAYDB_RUN` to one
of `api`, `capture`, `delivery`, or `demo-commerce` per service. Compose keeps
using its explicit image targets and is unaffected.

The source PostgreSQL service builds from `docker/postgres`; attach a persistent
volume at `/var/lib/postgresql/data` before its first deploy. It contains the
logical-replication configuration and initializes the commerce schema plus the
`relaydb_pub` publication.

## Required variables

Generate independent production secrets. Do not use Compose development
defaults outside local development.

| Service | Variables |
|---|---|
| API | `RELAYDB_RUN=api`, `RELAYDB_METADATA_DB_URL`, `RELAYDB_MASTER_KEY`, `RELAYDB_ADMIN_KEY_ID`, `RELAYDB_ADMIN_KEY`, `RELAYDB_READER_KEY_ID`, `RELAYDB_READER_KEY` |
| Capture | `RELAYDB_RUN=capture`, `RELAYDB_METADATA_DB_URL`, `RELAYDB_SOURCE_DB_URL`, `RELAYDB_SOURCE_NAME=demo`, `RELAYDB_CAPTURE_OWNER_ID`, `RELAYDB_MASTER_KEY` |
| Delivery | `RELAYDB_RUN=delivery`, `RELAYDB_METADATA_DB_URL`, `RELAYDB_MASTER_KEY` |
| Demo commerce | `RELAYDB_RUN=demo-commerce`, `RELAYDB_SOURCE_DB_URL` |
| Source PostgreSQL | `POSTGRES_USER`, `POSTGRES_PASSWORD`, `POSTGRES_DB=commerce` |
| Vercel dashboard | `RELAYDB_API_URL=https://<api-domain>`, `RELAYDB_READER_KEY_ID`, `RELAYDB_READER_KEY` |

Set `RELAYDB_SOURCE_DB_URL` to the Railway private source-database connection
string with `sslmode=disable`. Set `RELAYDB_METADATA_DB_URL` from the Railway
Postgres `DATABASE_URL` reference. Railway injects `PORT`; API and demo services
automatically bind to it when `RELAYDB_HTTP_ADDR` is unset.

## Current hosted-deploy blocker

Vercel and Railway CLIs are authenticated on this workstation. Railway project
creation was attempted on 2026-08-18, but the account reported: `Your trial has
expired. Please select a plan to continue using Railway.` No Railway project or
billable resource was created.

After enabling a Railway plan, create the project and services, attach the
source database volume, set the variables above, then deploy the source service
from `docker/postgres` and all RelayDB runtime services from the repository root.
Finally deploy `dashboard/` to Vercel with the three dashboard variables. The
public Vercel URL can then be used as the clickable product link.