// Package db owns the connection pool and is the only way to run queries.
//
// All work happens inside a transaction that carries the tenant context
// (app.org_id / app.user_id) as transaction-local settings, so Postgres row-level
// security can enforce isolation and nothing leaks between pooled connections.
// The raw pool is deliberately not exported.
package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"dentalsaas/internal/store"
)

// Tenant is the context a transaction runs under. Zero values are left unset,
// which makes tenant-owned tables return no rows (fail closed).
type Tenant struct {
	OrgID  uuid.UUID
	UserID uuid.UUID
}

// Tx is a transaction: generated queries plus the raw pgx.Tx for the rare hand-written query.
type Tx struct {
	*store.Queries
	PG pgx.Tx
}

type Pool struct {
	pool *pgxpool.Pool
}

func Open(ctx context.Context, url string) (*Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	p, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("open pool: %w", err)
	}
	return &Pool{pool: p}, nil
}

func (p *Pool) Close() { p.pool.Close() }

func (p *Pool) Ping(ctx context.Context) error { return p.pool.Ping(ctx) }

// InTenantTx runs fn in a transaction scoped to the tenant. fn's error rolls back.
func (p *Pool) InTenantTx(ctx context.Context, t Tenant, fn func(tx *Tx) error) error {
	return p.inTx(ctx, t, fn)
}

// InAuthTx runs fn with no tenant context: for global auth tables (sessions, login attempts,
// password resets) and the SECURITY DEFINER lookup functions used before a tenant is known.
func (p *Pool) InAuthTx(ctx context.Context, fn func(tx *Tx) error) error {
	return p.inTx(ctx, Tenant{}, fn)
}

func (p *Pool) inTx(ctx context.Context, t Tenant, fn func(tx *Tx) error) (err error) {
	pgtx, err := p.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() {
		if err != nil {
			_ = pgtx.Rollback(context.WithoutCancel(ctx))
		}
	}()

	if t.OrgID != uuid.Nil {
		if _, err = pgtx.Exec(ctx, "SELECT set_config('app.org_id', $1, true)", t.OrgID.String()); err != nil {
			return fmt.Errorf("set org context: %w", err)
		}
	}
	if t.UserID != uuid.Nil {
		if _, err = pgtx.Exec(ctx, "SELECT set_config('app.user_id', $1, true)", t.UserID.String()); err != nil {
			return fmt.Errorf("set user context: %w", err)
		}
	}

	if err = fn(&Tx{Queries: store.New(pgtx), PG: pgtx}); err != nil {
		return err
	}
	if err = pgtx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// SetUser switches app.user_id inside a running transaction. Used by flows that only learn
// the user part-way through, such as accepting an invitation.
func (tx *Tx) SetUser(ctx context.Context, userID uuid.UUID) error {
	_, err := tx.PG.Exec(ctx, "SELECT set_config('app.user_id', $1, true)", userID.String())
	return err
}

// SetOrg switches app.org_id inside a running transaction.
func (tx *Tx) SetOrg(ctx context.Context, orgID uuid.UUID) error {
	_, err := tx.PG.Exec(ctx, "SELECT set_config('app.org_id', $1, true)", orgID.String())
	return err
}

// IsNoRows reports whether err means a query matched nothing (including rows hidden by RLS).
func IsNoRows(err error) bool { return errors.Is(err, pgx.ErrNoRows) }
