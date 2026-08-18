# RelayDB multi-stage Dockerfile

FROM golang:1.26-alpine AS builder

WORKDIR /build

# Copy go mod files first for better caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source
COPY . .

# Build all binaries
RUN CGO_ENABLED=0 GOOS=linux go build -o /bin/api ./cmd/api
RUN CGO_ENABLED=0 GOOS=linux go build -o /bin/capture ./cmd/capture
RUN CGO_ENABLED=0 GOOS=linux go build -o /bin/delivery ./cmd/delivery
RUN CGO_ENABLED=0 GOOS=linux go build -o /bin/relayctl ./cmd/relayctl
RUN CGO_ENABLED=0 GOOS=linux go build -o /bin/loadgen ./cmd/loadgen
RUN CGO_ENABLED=0 GOOS=linux go build -o /bin/demo-commerce ./cmd/demo-commerce

# API image
FROM alpine:latest AS api
RUN apk --no-cache add ca-certificates wget
COPY --from=builder /bin/api /api
EXPOSE 8080 9090 2112
ENTRYPOINT ["/api"]

# Capture image
FROM alpine:latest AS capture
RUN apk --no-cache add ca-certificates wget
COPY --from=builder /bin/capture /capture
EXPOSE 2112
ENTRYPOINT ["/capture"]

# Delivery image
FROM alpine:latest AS delivery
RUN apk --no-cache add ca-certificates wget
COPY --from=builder /bin/delivery /delivery
EXPOSE 2112
ENTRYPOINT ["/delivery"]

# CLI image
FROM alpine:latest AS relayctl
RUN apk --no-cache add ca-certificates
COPY --from=builder /bin/relayctl /relayctl
ENTRYPOINT ["/relayctl"]

# Loadgen image
FROM alpine:latest AS loadgen
RUN apk --no-cache add ca-certificates
COPY --from=builder /bin/loadgen /loadgen
ENTRYPOINT ["/loadgen"]

# Demo commerce image
FROM alpine:latest AS demo-commerce
RUN apk --no-cache add ca-certificates
COPY --from=builder /bin/demo-commerce /demo-commerce
ENTRYPOINT ["/demo-commerce"]

# Dashboard build stage
FROM node:24-alpine AS dashboard-builder
WORKDIR /build
COPY dashboard/package.json dashboard/pnpm-lock.yaml* ./
RUN corepack enable && corepack prepare pnpm@latest --activate
RUN pnpm install --frozen-lockfile || pnpm install
COPY dashboard/ .
RUN pnpm build

# Dashboard image
FROM node:24-alpine AS dashboard
WORKDIR /app
COPY --from=dashboard-builder /build/.next/standalone ./
COPY --from=dashboard-builder /build/.next/static ./.next/static
COPY --from=dashboard-builder /build/public ./public
EXPOSE 3000
ENTRYPOINT ["node", "server.js"]