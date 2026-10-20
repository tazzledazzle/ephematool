//go:build integration

// Package testenv starts real Postgres and Redis containers once per test
// binary and exposes them with the production role split: the owner migrates,
// the ephematool_app role runs queries. Tests that write audit_events must use
// unique actors, because that table can never be truncated.
package testenv

import (
	"context"
	"net/url"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"

	"github.com/ephematool/ephematool/internal/store"
)

// Env is the shared infrastructure for one test.
type Env struct {
	OwnerURL, AppURL string
	Owner, App       *pgxpool.Pool  // owner (migrations, DDL checks) and app role
	Queries          *store.Queries // bound to the app role
	Redis            *redis.Client  // FLUSHALL on every Start
	RedisAddr        string
}

var shared struct {
	once      sync.Once
	ownerURL  string
	appURL    string
	redisAddr string
	err       error
}

func startContainers() {
	ctx := context.Background()
	pg, err := tcpostgres.Run(ctx, "postgres:18.6-alpine",
		tcpostgres.WithDatabase("ephematool"),
		tcpostgres.WithUsername("ephematool"),
		tcpostgres.WithPassword("ephematool"),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		shared.err = err
		return
	}
	if shared.ownerURL, shared.err = pg.ConnectionString(ctx, "sslmode=disable"); shared.err != nil {
		return
	}
	// Migrate once, as the owner. This also creates the ephematool_app role.
	pool, err := store.OpenUntraced(ctx, shared.ownerURL)
	if err != nil {
		shared.err = err
		return
	}
	defer pool.Close()
	if shared.err = store.Migrate(ctx, pool); shared.err != nil {
		return
	}
	shared.appURL, shared.err = WithCredentials(shared.ownerURL, "ephematool_app", "ephematool_app")
	if shared.err != nil {
		return
	}
	rc, err := tcredis.Run(ctx, "redis:8.8-alpine")
	if err != nil {
		shared.err = err
		return
	}
	uri, err := rc.ConnectionString(ctx)
	if err != nil {
		shared.err = err
		return
	}
	opt, err := redis.ParseURL(uri)
	if err != nil {
		shared.err = err
		return
	}
	shared.redisAddr = opt.Addr
}

// WithCredentials swaps the user and password of a Postgres URL.
func WithCredentials(dsn, user, password string) (string, error) {
	u, err := url.Parse(dsn)
	if err != nil {
		return "", err
	}
	u.User = url.UserPassword(user, password)
	return u.String(), nil
}

// WithDatabase swaps the database name of a Postgres URL.
func WithDatabase(dsn, name string) (string, error) {
	u, err := url.Parse(dsn)
	if err != nil {
		return "", err
	}
	u.Path = "/" + name
	return u.String(), nil
}

// Start returns the shared environment, skipping the test when Docker is not
// available. Redis is flushed so tests do not see each other's keys.
func Start(t *testing.T) *Env {
	t.Helper()
	shared.once.Do(startContainers)
	if shared.err != nil {
		t.Skipf("containers unavailable: %v", shared.err)
	}
	ctx := context.Background()
	owner, err := store.Open(ctx, shared.ownerURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(owner.Close)
	app, err := store.Open(ctx, shared.appURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(app.Close)
	rdb := redis.NewClient(&redis.Options{Addr: shared.redisAddr})
	t.Cleanup(func() { _ = rdb.Close() })
	if err := rdb.FlushAll(ctx).Err(); err != nil {
		t.Fatal(err)
	}
	return &Env{
		OwnerURL: shared.ownerURL, AppURL: shared.appURL,
		Owner: owner, App: app, Queries: store.New(app),
		Redis: rdb, RedisAddr: shared.redisAddr,
	}
}
