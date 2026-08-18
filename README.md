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

# Open the live operations dashboard
# http://localhost:3000

# Start the demo commerce order API
docker compose --profile demo up -d demo-commerce

# Create an order to generate CDC events
curl -X POST http://localhost:8081/orders \
	-H "Content-Type: application/json" \
	-d '{"customer_id":1,"items":[{"product_id":1,"quantity":1}]}'

# Watch events
relayctl events tail --source demo --table orders
```

## Deployment

The dashboard is designed for Vercel, with the stateful backend deployed on a
container host. See [docs/deployment.md](docs/deployment.md) for the verified
local topology, production environment variables, and the current Railway
account prerequisite.

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