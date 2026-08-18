# RelayDB Demo Scenes

This document describes the failure and recovery demos that prove RelayDB's crash-correctness guarantees.

## Prerequisites

```bash
# Start the full stack
docker compose up -d

# Verify all services are healthy
docker compose ps
```

## Scene 1: Basic Capture (§71)

**Goal**: Write to PostgreSQL and see a durable normalized event.

```bash
# Terminal 1: Watch events
relayctl events tail --source demo --table orders

# Terminal 2: Create an order
curl -X POST http://localhost:8080/api/v1/demo/orders \
  -H "Content-Type: application/json" \
  -d '{"customer_id": 1, "items": [{"product_id": 1, "quantity": 2}]}'
```

**Expected**: The `events tail` command shows 3 events (order + 2 order_items) grouped under one transaction.

## Scene 2: Duplicate WAL Replay (§73)

**Goal**: Prove crash-after-commit-before-ACK produces no duplicates.

```bash
# Terminal 1: Watch for duplicates
relayctl events tail --source demo --format json | jq -c 'select(.duplicate == true)'

# Terminal 2: Simulate crash at the critical window
# (Requires test hook: RELAYDB_TEST_CRASH_AFTER_COMMIT=true)
docker compose restart capture-1

# Verify: No duplicate events appear
```

**Expected**: Events are replayed but deduplicated by `(source_id, commit_end_lsn, sequence_number)`.

## Scene 3: Source Restart (§63)

**Goal**: Capture resumes after PostgreSQL restart.

```bash
# Restart the source database
docker compose restart source-postgres

# Watch capture logs
docker compose logs -f capture-1

# Verify: Capture reconnects and continues without data loss
relayctl source status demo
```

**Expected**: Capture reconnects via backoff, resumes from last checkpoint, no events lost.

## Scene 4: Metadata Outage (§64)

**Goal**: Source WAL is retained when metadata DB is unavailable.

```bash
# Stop metadata database
docker compose stop metadata-postgres

# Generate load (will backpressure)
go run ./cmd/demo-commerce -rate=100 &

# Observe: WAL grows on source (no acks sent)
docker exec relaydb-source-postgres-1 psql -U relaydb -c "SELECT * FROM pg_replication_slots;"

# Restart metadata
docker compose start metadata-postgres

# Verify: Capture resumes, no data loss
```

**Expected**: Capture stops acking, source retains WAL, recovery resumes without loss.

## Scene 5: Stale Consumer Fencing (§74)

**Goal**: Stale consumer cannot commit offsets after lease expiry.

```bash
# Start two consumers in the same group
relayctl consumer poll --group analytics --member consumer-1 &
relayctl consumer poll --group analytics --member consumer-2 &

# Kill consumer-1 mid-batch (simulating crash)
# Consumer-2 should take over the partition

# Try to ACK with consumer-1's stale token (should fail)
```

**Expected**: Stale ACK is rejected with generation mismatch; offset only advances under valid lease.

## Scene 6: Large Transaction (§75)

**Goal**: 100k-row transaction is captured within memory bounds.

```bash
# Generate a large transaction
psql postgres://relaydb:relaydb@localhost:5432/commerce -c "
BEGIN;
INSERT INTO order_items (order_id, product_id, quantity, price_cents)
SELECT 1, 1, 1, 100 FROM generate_series(1, 100000);
COMMIT;
"

# Verify capture processes it
relayctl events count --transaction <xid>
```

**Expected**: Transaction captured atomically; memory stays bounded; no OOM.

## Scene 7: Webhook Retry and DLQ (§76)

**Goal**: Failed webhook delivery retries, then dead-letters.

```bash
# Start a webhook receiver that fails
docker run -p 9000:8080 --network relaydb_default mockserver/mockserver

# Configure a sink to the failing endpoint
relayctl sink create --url http://localhost:9000/fail --source demo

# Generate events
go run ./cmd/demo-commerce -rate=10 &

# Watch retry attempts
relayctl dlq list

# Manually retry after fixing receiver
relayctl dlq retry <id>
```

**Expected**: Exponential backoff retry, DLQ entry after max attempts, manual retry succeeds.

## Scene 8: Transaction Explorer (§76 Visual Proof)

**Goal**: Dashboard shows multi-table transaction as one atomic unit.

1. Open http://localhost:3000/transactions
2. Find a transaction with 3+ events
3. Verify events are ordered and grouped
4. Verify before/after images are correctly displayed

**Expected**: Transaction boundary is visually obvious; events show correct order.