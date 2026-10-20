// Package store owns Postgres access: pool, embedded goose migrations and the
// sqlc-generated queries.
package store

import (
	"context"
	"embed"
	"fmt"
	"io/fs"

	"github.com/exaring/otelpgx"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Open creates a traced pgx pool.
func Open(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	cfg.ConnConfig.Tracer = otelpgx.NewTracer()
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return pool, nil
}

// OpenUntraced creates a small pool without the otelpgx tracer. The status
// prober and the migrator use it so those calls do not create traces.
func OpenUntraced(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	cfg.MaxConns = 2 // the session locker holds one connection while goose uses another
	return pgxpool.NewWithConfig(ctx, cfg)
}

// Migrate applies the embedded goose migrations under a Postgres advisory
// lock (WR-11), so two processes starting together cannot race. It must run
// with the OWNER connection (MIGRATE_DATABASE_URL), never the app role.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := MigrateVersion(ctx, pool)
	return err
}

// MigrateVersion is Migrate plus the resulting schema version, for logging.
func MigrateVersion(ctx context.Context, pool *pgxpool.Pool) (int64, error) {
	sub, err := fs.Sub(migrations, "migrations")
	if err != nil {
		return 0, err
	}
	locker, err := lock.NewPostgresSessionLocker()
	if err != nil {
		return 0, err
	}
	db := stdlib.OpenDBFromPool(pool)
	defer db.Close()
	provider, err := goose.NewProvider(goose.DialectPostgres, db, sub, goose.WithSessionLocker(locker))
	if err != nil {
		return 0, err
	}
	if _, err := provider.Up(ctx); err != nil {
		return 0, err
	}
	return provider.GetDBVersion(ctx)
}
