# RelayDB Benchmarks

This directory contains performance benchmarks and methodology documentation.

## Methodology

All benchmarks are run against a Docker Compose stack with:

- PostgreSQL 16 (source) with logical replication enabled
- PostgreSQL 16 (metadata)
- RelayDB capture, api, and delivery services
- Prometheus for metrics collection

### Scenarios

| Scenario | Writers | Tx/s | Rows/Tx | Duration |
|----------|---------|------|---------|----------|
| A - Light | 1 | 10 | 10 | 60s |
| B - Medium | 10 | 100 | 50 | 60s |
| C - Heavy | 50 | 500 | 100 | 60s |

### Metrics Captured

- `relaydb_capture_lag_bytes` - WAL lag in bytes
- `relaydb_capture_lag_seconds` - Capture lag in seconds
- `relaydb_events_total` - Events captured per second
- `relaydb_persist_duration_seconds` - Persistence latency (native histogram)

## Running Benchmarks

```bash
# Start the benchmark stack
docker compose -f docker-compose.bench.yml up -d

# Run scenario A
go run ./cmd/loadgen -scenario=a -duration=60s

# Collect results
curl http://localhost:9091/api/v1/query?query=relaydb_events_total
```

## Results

Results are published in this directory with environment details:

- [2026-08-18-initial-results.md](2026-08-18-initial-results.md) - Initial Phase 7 results

## Hardware

All results include:
- CPU model and count
- RAM total and available
- Disk type (SSD/NVMe)
- OS and kernel version
- Docker version and resources allocated