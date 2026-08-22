.PHONY: build test race lint fmt vet compose-config compose-up compose-down clean

GO ?= go
BINARIES := api capture delivery relayctl loadgen demo-commerce

build:
	$(GO) build -o bin/ ./cmd/...

test:
	$(GO) test ./...

race:
	$(GO) test -race ./...

# Local pre-push ritual (CI replacement): vet + full suite including the
# end-to-end WAL proof (~35s, needs Docker).
verify:
	$(GO) vet ./...
	$(GO) test -count=1 ./...

lint:
	golangci-lint run ./...

fmt:
	$(GO) fmt ./...

vet:
	$(GO) vet ./...

compose-config:
	docker compose config

compose-up:
	docker compose up -d

compose-down:
	docker compose down

clean:
	rm -rf bin/

# Run integration tests (requires Docker)
test-integration:
	$(GO) test -tags=integration ./tests/integration/...

# Run failure tests (requires Docker)
test-failure:
	$(GO) test -tags=integration ./tests/failure/...

# Run all tests including integration
test-all:
	$(GO) test -tags=integration ./...

# Generate protobuf
proto:
	buf generate

# Database migrations
migrate-up:
	$(GO) run ./cmd/api -migrate

migrate-down:
	@echo "Down migrations not implemented"