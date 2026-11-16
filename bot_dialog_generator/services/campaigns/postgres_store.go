package main

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresStore keeps the do-not-contact list and the delivery log in the tables of
// studio-api's migration 000002. Like studio-api, it connects as a role that owns
// nothing and declares its workspace in every transaction, so row-level security keeps
// one area's recipients away from another's.
type PostgresStore struct {
	pool *pgxpool.Pool
}

// NewPostgresStore connects with databaseURL, which must be a member of bdg_app.
func NewPostgresStore(ctx context.Context, databaseURL string) (*PostgresStore, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("postgres: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres: %w", err)
	}
	return &PostgresStore{pool: pool}, nil
}

func (s *PostgresStore) Close() { s.pool.Close() }

func (s *PostgresStore) inWorkspace(ctx context.Context, workspaceID string, fn func(pgx.Tx) error) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "SELECT set_config('app.workspace_id', $1, true)", workspaceID); err != nil {
			return err
		}
		return fn(tx)
	})
}

func (s *PostgresStore) IsSuppressed(ctx context.Context, workspaceID, emailHash string) (bool, error) {
	var suppressed bool
	err := s.inWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM campaign_suppressions WHERE workspace_id = $1 AND email_hash = $2)",
			workspaceID, emailHash).Scan(&suppressed)
	})
	return suppressed, err
}

func (s *PostgresStore) Suppress(ctx context.Context, workspaceID, emailHash, reason string) error {
	return s.inWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO campaign_suppressions (workspace_id, email_hash, reason) VALUES ($1, $2, $3)
			ON CONFLICT (workspace_id, email_hash) DO NOTHING`, workspaceID, emailHash, reason)
		return err
	})
}

func (s *PostgresStore) RecordSend(ctx context.Context, workspaceID, campaignID, emailHash, status string) error {
	return s.inWorkspace(ctx, workspaceID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "INSERT INTO campaign_sends (workspace_id, campaign_id, email_hash, status) VALUES ($1, $2, $3, $4)",
			workspaceID, campaignID, emailHash, status)
		return err
	})
}

var _ Store = (*PostgresStore)(nil)
