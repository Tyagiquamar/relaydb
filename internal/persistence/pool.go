package persistence

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/tyagiquamar/relaydb/internal/telemetry"
)

// Pool wraps pgxpool.Pool with RelayDB-specific configuration.
type Pool struct {
	pool *pgxpool.Pool
}

// Config holds pool configuration.
type Config struct {
	URL                 string
	MinConns            int32
	MaxConns            int32
	MaxConnLifetime     time.Duration
	MaxConnLifetimeJitter time.Duration
	MaxConnIdleTime     time.Duration
	HealthCheckPeriod   time.Duration
}

// DefaultConfig returns production-tuned defaults.
func DefaultConfig(url string) Config {
	return Config{
		URL:                   url,
		MinConns:              2,
		MaxConns:              10,
		MaxConnLifetime:       time.Hour,
		MaxConnLifetimeJitter: 10 * time.Minute,
		MaxConnIdleTime:       30 * time.Minute,
		HealthCheckPeriod:     time.Minute,
	}
}

// NewPool creates a configured connection pool.
func NewPool(ctx context.Context, cfg Config) (*Pool, error) {
	poolCfg, err := pgxpool.ParseConfig(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("parse pool config: %w", err)
	}

	poolCfg.MinConns = cfg.MinConns
	poolCfg.MaxConns = cfg.MaxConns
	poolCfg.MaxConnLifetime = cfg.MaxConnLifetime
	poolCfg.MaxConnLifetimeJitter = cfg.MaxConnLifetimeJitter
	poolCfg.MaxConnIdleTime = cfg.MaxConnIdleTime
	poolCfg.HealthCheckPeriod = cfg.HealthCheckPeriod

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}

	// Verify connectivity
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping pool: %w", err)
	}

	// Export pool stats
	exportPoolStats(pool)

	return &Pool{pool: pool}, nil
}

// Close closes the pool.
func (p *Pool) Close() {
	p.pool.Close()
}

// Acquire gets a connection from the pool.
func (p *Pool) Acquire(ctx context.Context) (*pgxpool.Conn, error) {
	return p.pool.Acquire(ctx)
}

// Begin starts a transaction.
func (p *Pool) Begin(ctx context.Context) (pgx.Tx, error) {
	return p.pool.Begin(ctx)
}

// BeginTx starts a transaction with options.
func (p *Pool) BeginTx(ctx context.Context, txOptions pgx.TxOptions) (pgx.Tx, error) {
	return p.pool.BeginTx(ctx, txOptions)
}

// Query executes a query.
func (p *Pool) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return p.pool.Query(ctx, sql, args...)
}

// QueryRow executes a query that returns at most one row.
func (p *Pool) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return p.pool.QueryRow(ctx, sql, args...)
}

// Exec executes a statement.
func (p *Pool) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	return p.pool.Exec(ctx, sql, args...)
}

// Stats returns pool statistics.
func (p *Pool) Stats() *pgxpool.Stat {
	return p.pool.Stat()
}

// exportPoolStats registers pool metrics.
func exportPoolStats(pool *pgxpool.Pool) {
	stats := pool.Stat()
	
	// Register gauges for pool stats
	telemetry.Registry.MustRegister(
		prometheus.NewGaugeFunc(
			prometheus.GaugeOpts{
				Namespace: "relaydb",
				Name:      "pgx_pool_connections_open",
				Help:      "Number of open connections in the pool.",
			},
			func() float64 { return float64(stats.TotalConns()) },
		),
		prometheus.NewGaugeFunc(
			prometheus.GaugeOpts{
				Namespace: "relaydb",
				Name:      "pgx_pool_connections_idle",
				Help:      "Number of idle connections in the pool.",
			},
			func() float64 { return float64(stats.IdleConns()) },
		),
		prometheus.NewGaugeFunc(
			prometheus.GaugeOpts{
				Namespace: "relaydb",
				Name:      "pgx_pool_connections_used",
				Help:      "Number of in-use connections in the pool.",
			},
			func() float64 { return float64(stats.AcquiredConns()) },
		),
	)
}