# RelayDB — Session Handoff (2026-08-18)

**Repo:** `D:\pers\relaydb` (git, branch `main`, no remote)
**Plan being executed:** [docs/plans/2026-08-18-001-feat-relaydb-cdc-platform-plan.md](docs/plans/2026-08-18-001-feat-relaydb-cdc-platform-plan.md)
**State:** All 14 units coded. **NOT deployable end-to-end.** Three blockers listed below.

---

## 1. Environment (critical — new session must redo this)

- **Go 1.26.5** is NOT on PATH permanently. It lives at `D:\pers\.tools\go` (zip install — winget MSI failed with error 1618). Every terminal needs:
  ```powershell
  $env:Path = "D:\pers\.tools\go\bin;" + $env:Path
  $env:GOROOT = "D:\pers\.tools\go"
  ```
- **buf 1.72.0** installed via winget (on PATH after session refresh).
- **Docker Compose v5.3.1** present. Node 24, pnpm 11.8 present.
- Module: `github.com/tyagiquamar/relaydb`. pglogrepl pinned to pseudo-version `v0.0.0-20260401131349-e37c41485510`.
- Migrations exist in BOTH `migrations/001_init.sql` (canonical) and `internal/persistence/migrations/001_init.sql` (embedded copy for the Go migrator — must be kept in sync manually).

## 2. Git history

```
8fda552 feat(U6-U14): complete RelayDB platform   <- bulk commit, 43 files
50ef8d3 feat(U5): checkpoint state machine + capture service
b1261fa feat(U3+U4): replication client, decoder, transaction buffer
96d641b feat(U2): metadata schema, persistence, crypto envelope
c833827 feat(U1): repository scaffold, config, telemetry skeleton, CI
```

`go build ./...`, `go vet ./...`, `go test ./internal/...` all pass (unit tests only — no testcontainers run yet). `docker compose config` validates.

## 3. What's complete (compiles, unit-tested where applicable)

| Unit | Status | Notes |
|------|--------|-------|
| U1 scaffold/CI | ✅ | compose, Dockerfile, CI workflow, config, telemetry |
| U2 schema/persistence/crypto | ✅ | Full DDL for all tables; AES-GCM envelope with AAD; migrator (embed.FS) |
| U3 replication client/decoder | ✅ | pgoutput v1 decode, relation cache, 3-state columns (value/null/unchanged_toast), single-goroutine conn, keepalive replies |
| U4 tx buffer | ✅ | xid-keyed buffer, sequence numbers, ULIDs, byte/event/inflight bounds |
| U5 capture + checkpoint | ⚠️ coded | `internal/capture/service.go` has full ingest (COPY-to-staging, guarded insert, fenced checkpoint) but **not wired to a binary** |
| U6 REST API + CLI + demo app | ⚠️ partial | API handlers exist; source create is TODO (no encryption/validation wired); relayctl subcommands are stubs; demo-commerce complete |
| U7 failure harness | ⚠️ scaffolding | testcontainers harness + crash-window tests are **placeholders** (`t.Log` only) |
| U8 partitions/leases | ✅ coded | FNV-1a hash, lease claim/heartbeat/fencing SQL — no tests |
| U9 gRPC consumer API | ⚠️ coded | proto + buf codegen done (`gen/`), server implements Poll/Ack/Nack/Heartbeat; consumer poll partition filter is naive (over-fetch then filter) |
| U10 replay | ⚠️ coded | Session CRUD only; **no replay execution engine** (no cursor loop, no JSONL export, no webhook routing) |
| U11 webhook/DLQ | ⚠️ coded | Deliverer with HMAC sign, backoff, status classification; SSRF dialer only sketched; DLQ store CRUD only; **no delivery service loop** claiming pending deliveries |
| U12 observability | ⚠️ skeleton | Registry + OTel provider exist; no capture/consumer/delivery instrumentation wired |
| U13 dashboard | ⚠️ static | Pages exist but fetch `/api/v1/*` directly — no BFF/proxy; **not in compose** |
| U14 loadgen | ✅ coded | Scenarios light/medium/heavy/burst; **never run** |

## 4. BLOCKERS — what to do next, in order

### Blocker 1: Wire capture into the binary (highest priority)
`cmd/capture/main.go` is a placeholder ("replication not yet implemented"). Fix:
1. Connect metadata pool (`persistence.NewPool`), run migrator.
2. Look up the source row by name (env `RELAYDB_SOURCE_NAME`) to get `source_id` — capture.Service.Run takes a sourceID but there is no lookup code.
3. Instantiate `capture.NewService(cfg, pool)` and call `Run(ctx, sourceID)`.
4. Watch for runtime bugs in `internal/capture/service.go`: `cdc_transactions.xid` is `xid8` in DDL but code inserts `fmt.Sprintf("%d", xid)` (string) — likely fails; fix to `pguint64`/text cast or change DDL to `bigint`.

### Blocker 2: Dashboard in compose + API proxy
1. Add `dashboard` service to `docker-compose.yml` (target `dashboard` in Dockerfile exists), port `3000:3000`, env `RELAYDB_API_URL=http://api:8080`.
2. Dashboard pages call relative `/api/v1/*` — add Next.js `rewrites()` in `dashboard/next.config.mjs` proxying `/api/v1/:path*` to `process.env.RELAYDB_API_URL`, and send the reader API key server-side (BFF pattern per KTD-25 — never in browser JS).
3. The API has **no `/api/v1/stats`, `/api/v1/dlq` endpoints** that the dashboard calls — add them to `internal/api/server.go`.

### Blocker 3: Smoke-run against real PG and fix breakage
Nothing has run against real PostgreSQL. Expect failures in: `pg_lsn::text` scans, ULID `bytea` handling in API list endpoints (`id` scanned as string — will break, needs hex encode), JSONB scans into `json.RawMessage`, `xid8` type, staging-table COPY column order. Run:
```powershell
docker compose up -d source-postgres metadata-postgres
go run ./cmd/api   # with RELAYDB_METADATA_DB_URL set
go run ./cmd/capture
```

### Then (functional gaps)
- **Source registration**: `handleCreateSource` must encrypt credentials via `internal/crypto` envelope and validate replica identity (`validateSource` is a stub returning nil).
- **Delivery loop**: `cmd/delivery/main.go` is a placeholder — needs a loop claiming `delivery_attempts` (SKIP LOCKED) and calling `webhook.Deliverer`, writing DLQ on exhaustion.
- **Replay engine**: `internal/replay` needs the batch read/dispatch loop honoring cursor + destination.
- **Consumer redelivery**: `Nack` currently just logs — needs attempt tracking + poison policy (DLQ|HALT).
- **Tests**: fill in `tests/failure/*` and `tests/integration/*` placeholders (currently `t.Log` stubs behind `//go:build integration`).

## 5. Conventions to preserve

- No PG enums — text + CHECK constraints (COPY compatibility, KTD-6).
- Event identity `(source_id, commit_end_lsn, sequence_number)`; ULID `bytea` PK (KTD-5/6).
- Never ACK WAL past `persisted_lsn`; status updates report flushed only (KTD-3).
- One goroutine owns the replication pgconn; metadata goes through pgxpool (KTD-4/13).
- Fenced writes: owner + generation + unexpired lease + LSN monotonicity, zero-rows = rollback (KTD-14/16).
- Commit per unit with conventional messages; do NOT edit the plan body.
- User preference: do NOT run tests after each slice — code through, validate once at the end.

## 6. Quick resume checklist

```powershell
cd D:\pers\relaydb
$env:Path = "D:\pers\.tools\go\bin;" + $env:Path; $env:GOROOT = "D:\pers\.tools\go"
go build ./...          # should be clean
git log --oneline       # expect 8fda552 at HEAD
# then start at Blocker 1 above
```
