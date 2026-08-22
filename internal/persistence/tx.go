package persistence

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// TxFunc is a function that runs within a transaction.
type TxFunc func(tx pgx.Tx) error

// WithTx executes fn within a transaction, committing on success or rolling back on error.
// This ensures the transaction is always closed, even on panic.
func WithTx(ctx context.Context, pool *Pool, fn TxFunc) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}

	defer func() {
		// Rollback is safe to call even after commit (no-op)
		_ = tx.Rollback(ctx)
	}()

	if err := fn(tx); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}

	return nil
}

// WithTxOptions executes fn within a transaction with options.
func WithTxOptions(ctx context.Context, pool *Pool, opts pgx.TxOptions, fn TxFunc) error {
	tx, err := pool.BeginTx(ctx, opts)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}

	defer func() {
		_ = tx.Rollback(ctx)
	}()

	if err := fn(tx); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}

	return nil
}

// QueryRowTx is a helper for querying a single row within a transaction.
func QueryRowTx(ctx context.Context, tx pgx.Tx, sql string, args ...any) pgx.Row {
	return tx.QueryRow(ctx, sql, args...)
}

// ExecTx is a helper for executing a statement within a transaction.
func ExecTx(ctx context.Context, tx pgx.Tx, sql string, args ...any) (pgconn.CommandTag, error) {
	return tx.Exec(ctx, sql, args...)
}
