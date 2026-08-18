# RelayDB — PostgreSQL CDC and Replay Platform

RelayDB is a production-style change-data-capture (CDC) and replay platform built on PostgreSQL logical replication. It provides crash-safe event capture with LSN checkpointing, consumer groups with partition leases and fencing, replay sessions, webhook delivery with retry/DLQ, and full observability.

## Features

- **Crash-safe capture**: Never acknowledges WAL until events are durably persisted
- **At-least-once delivery**: Honest semantics with idempotency keys and fencing tokens
- **Consumer groups**: Partition leases with automatic failover
- **Replay**: Historical re-processing by time, LSN, or event ID
- **Webhook sinks**: HTTP delivery with retry, circuit breaker, and DLQ
- **Observability**: Prometheus metrics, OpenTelemetry traces, structured logs

## Quick Start

```bash
# Start all services (source PG, metadata PG, api, capture, delivery, dashboard)
docker compose up

# In another terminal, generate demo traffic
go run ./cmd/demo-commerce

# Watch events
relayctl events tail --source demo --table orders
```

## Architecture

See [docs/architecture.md](docs/architecture.md) for the full design.

## Development

```bash
# Run tests
make test

# Run with race detector
make race

# Lint
make lint

# Build all binaries
make build
```

## Guarantees

- **Durable at-least-once ingestion**: Events are persisted before WAL acknowledgment
- **No exactly-once side effects**: Consumers must handle idempotency
- **Replay bounded by retention**: Historical replay limited by configured retention

## License

MIT