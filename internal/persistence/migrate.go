package persistence

import (
	"context"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/tyagiquamar/relaydb/migrations"
)

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

// loadMigrations loads migrations from the embedded canonical migrations FS.
func loadMigrations() ([]Migration, error) {
	entries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		return nil, err
	}

	var list []Migration
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}

		// Parse version from filename: 001_init.sql -> 1
		var version int
		if _, err := fmt.Sscanf(entry.Name(), "%d", &version); err != nil {
			continue
		}

		data, err := migrations.FS.ReadFile(entry.Name())
		if err != nil {
			return nil, err
		}

		list = append(list, Migration{
			Version: version,
			Name:    entry.Name(),
			SQL:     string(data),
		})
	}

	// Sort by version
	sort.Slice(list, func(i, j int) bool {
		return list[i].Version < list[j].Version
	})

	return list, nil
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
// The SQL file contains '--' comments and multiple statements, so it is sent
// through the raw pgconn multi-statement simple protocol instead of tx.Exec,
// whose naive statement splitter breaks on semicolons inside comments.
func (m *Migrator) applyMigration(ctx context.Context, mig Migration) error {
	conn, err := m.pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire conn: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "BEGIN"); err != nil {
		return fmt.Errorf("begin: %w", err)
	}

	res := conn.Conn().PgConn().Exec(ctx, mig.SQL)
	if _, err := res.ReadAll(); err != nil {
		_, _ = conn.Exec(ctx, "ROLLBACK")
		return fmt.Errorf("execute %s: %w", mig.Name, err)
	}

	if _, err := conn.Exec(ctx,
		"INSERT INTO schema_migrations (version) VALUES ($1) ON CONFLICT DO NOTHING",
		mig.Version,
	); err != nil {
		_, _ = conn.Exec(ctx, "ROLLBACK")
		return fmt.Errorf("record migration: %w", err)
	}

	if _, err := conn.Exec(ctx, "COMMIT"); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}
