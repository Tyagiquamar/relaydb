package persistence

import (
	"context"
	"embed"
	"fmt"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migration represents a database migration.
type Migration struct {
	Version int
	Name    string
	SQL     string
}

// Migrator applies database migrations.
type Migrator struct {
	pool *Pool
}

// NewMigrator creates a migrator.
func NewMigrator(pool *Pool) *Migrator {
	return &Migrator{pool: pool}
}

// Migrate applies all pending migrations.
func (m *Migrator) Migrate(ctx context.Context) error {
	// Ensure migrations table exists
	_, err := m.pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version INT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)
	`)
	if err != nil {
		return fmt.Errorf("create migrations table: %w", err)
	}

	// Load migrations from embedded FS
	migrations, err := loadMigrations()
	if err != nil {
		return fmt.Errorf("load migrations: %w", err)
	}

	// Get applied versions
	applied, err := m.getAppliedVersions(ctx)
	if err != nil {
		return fmt.Errorf("get applied migrations: %w", err)
	}

	// Apply pending migrations
	for _, mig := range migrations {
		if applied[mig.Version] {
			continue
		}

		if err := m.applyMigration(ctx, mig); err != nil {
			return fmt.Errorf("apply migration %d: %w", mig.Version, err)
		}
	}

	return nil
}

// loadMigrations loads migrations from the embedded filesystem.
func loadMigrations() ([]Migration, error) {
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return nil, err
	}

	var migrations []Migration
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}

		// Parse version from filename: 001_init.sql -> 1
		var version int
		if _, err := fmt.Sscanf(entry.Name(), "%d", &version); err != nil {
			continue
		}

		data, err := migrationsFS.ReadFile("migrations/" + entry.Name())
		if err != nil {
			return nil, err
		}

		migrations = append(migrations, Migration{
			Version: version,
			Name:    entry.Name(),
			SQL:     string(data),
		})
	}

	// Sort by version
	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].Version < migrations[j].Version
	})

	return migrations, nil
}

// getAppliedVersions returns the set of applied migration versions.
func (m *Migrator) getAppliedVersions(ctx context.Context) (map[int]bool, error) {
	rows, err := m.pool.Query(ctx, "SELECT version FROM schema_migrations")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	applied := make(map[int]bool)
	for rows.Next() {
		var version int
		if err := rows.Scan(&version); err != nil {
			return nil, err
		}
		applied[version] = true
	}

	return applied, rows.Err()
}

// applyMigration applies a single migration within a transaction.
func (m *Migrator) applyMigration(ctx context.Context, mig Migration) error {
	return WithTx(ctx, m.pool, func(tx pgx.Tx) error {
		// Apply the migration SQL
		if _, err := tx.Exec(ctx, mig.SQL); err != nil {
			return fmt.Errorf("execute %s: %w", mig.Name, err)
		}

		// Record the migration
		if _, err := tx.Exec(ctx,
			"INSERT INTO schema_migrations (version) VALUES ($1)",
			mig.Version,
		); err != nil {
			return fmt.Errorf("record migration: %w", err)
		}

		return nil
	})
}