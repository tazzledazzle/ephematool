//go:build integration

package store_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/ephematool/ephematool/internal/store"
	"github.com/ephematool/ephematool/internal/testenv"
)

func sqlState(err error) string {
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		return pe.Code
	}
	return ""
}

func TestMigrateConcurrent(t *testing.T) {
	env := testenv.Start(t)
	ctx := context.Background()
	name := fmt.Sprintf("migrate_race_%d", time.Now().UnixNano())
	if _, err := env.Owner.Exec(ctx, "create database "+name); err != nil {
		t.Fatal(err)
	}
	dsn, _ := testenv.WithDatabase(env.OwnerURL, name)

	versions := make([]int64, 2)
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i := range versions {
		wg.Add(1)
		go func() {
			defer wg.Done()
			pool, err := store.OpenUntraced(ctx, dsn)
			if err != nil {
				errs[i] = err
				return
			}
			defer pool.Close()
			versions[i], errs[i] = store.MigrateVersion(ctx, pool)
		}()
	}
	wg.Wait()
	for i := range versions {
		if errs[i] != nil || versions[i] != 3 {
			t.Errorf("migrator %d: version %d err %v, want version 3 and no error", i, versions[i], errs[i])
		}
	}
}

func TestAppRoleGrants(t *testing.T) {
	env := testenv.Start(t)
	ctx := context.Background()
	if _, err := env.App.Exec(ctx, `insert into audit_events (actor_github_id, actor_login, action, outcome)
		values (9001, 'grants-test', 'environment.create', 'success')`); err != nil {
		t.Fatalf("app insert into audit_events: %v", err)
	}
	denied := []string{
		"update audit_events set reason = 'x'",
		"delete from audit_events",
		"truncate audit_events",
		"select * from goose_db_version",
	}
	for _, q := range denied {
		if _, err := env.App.Exec(ctx, q); sqlState(err) != "42501" {
			t.Errorf("%q as app role: SQLSTATE %q, want 42501 (permission denied): %v", q, sqlState(err), err)
		}
	}
	if _, err := env.App.Exec(ctx, "insert into users (github_id, login) values (9002, 'grants') on conflict do nothing"); err != nil {
		t.Errorf("app insert into users: %v", err)
	}
	if _, err := env.App.Exec(ctx, "update users set login = 'grants2' where github_id = 9002"); err != nil {
		t.Errorf("app update users: %v", err)
	}
}

func TestOwnerBlockedByTrigger(t *testing.T) {
	env := testenv.Start(t)
	ctx := context.Background()
	for _, q := range []string{"delete from audit_events", "truncate audit_events"} {
		_, err := env.Owner.Exec(ctx, q)
		if err == nil || !strings.Contains(err.Error(), "audit_events is append-only") {
			t.Errorf("%q as owner: err = %v, want the append-only trigger error", q, err)
		}
	}
}

func insertEnv(t *testing.T, env *testenv.Env, id string) {
	t.Helper()
	_, err := env.App.Exec(context.Background(), `insert into environments
		(id, owner_github_id, owner_login, repo, ref_type, ref, sha, image, template, chart_version, size,
		 correlation_id, traceparent, trace_id, expires_at)
		values ($1, 1, 'o', 'a/b', 'branch', 'main', repeat('a', 40), 'img', 'web-app', '0.1.0', 'S', 'c', 't', 'tid', now())`, id)
	if err != nil {
		t.Fatalf("insert %s: %v", id, err)
	}
}

func TestEnvironmentCAS(t *testing.T) {
	env := testenv.Start(t)
	ctx := context.Background()
	id := fmt.Sprintf("pv-%010d", time.Now().UnixNano()%1e10)
	insertEnv(t, env, id)
	q := env.Queries

	if n, err := q.MarkRunning(ctx, id); err != nil || n != 0 {
		t.Fatalf("MarkRunning on a requested row: n=%d err=%v, want 0", n, err)
	}
	if n, _ := q.ClaimDispatch(ctx, id); n != 1 {
		t.Fatalf("first ClaimDispatch = %d, want 1", n)
	}
	if n, _ := q.ClaimDispatch(ctx, id); n != 0 {
		t.Fatalf("second ClaimDispatch = %d, want 0 (no double dispatch)", n)
	}
	args := store.MarkProvisioningParams{ID: id, RunID: pgInt8(42), RunHtmlUrl: "http://run", ProvisionDeadlineAt: pgTime(time.Now().Add(time.Minute))}
	if n, _ := q.MarkProvisioning(ctx, args); n != 1 {
		t.Fatalf("first MarkProvisioning = %d, want 1", n)
	}
	if n, _ := q.MarkProvisioning(ctx, args); n != 0 {
		t.Fatalf("second MarkProvisioning = %d, want 0", n)
	}
	if n, _ := q.MarkRunning(ctx, id); n != 1 {
		t.Fatalf("MarkRunning = %d, want 1", n)
	}
	if n, _ := q.MarkFailed(ctx, store.MarkFailedParams{ID: id, FailureReason: "late"}); n != 0 {
		t.Fatalf("MarkFailed after running = %d, want 0 (forward-only)", n)
	}
	if _, err := env.App.Exec(ctx, "update environments set status = 'destroyed' where id = $1", id); sqlState(err) != "23514" {
		t.Fatalf("status 'destroyed' must violate the CHECK (23514), got %q", sqlState(err))
	}
}

func TestEnvironmentIDCheck(t *testing.T) {
	env := testenv.Start(t)
	for _, id := range []string{"pv-UPPER12345", "evil", "pv-short"} {
		_, err := env.App.Exec(context.Background(), `insert into environments
			(id, owner_github_id, owner_login, repo, ref_type, ref, sha, image, template, chart_version, size,
			 correlation_id, traceparent, trace_id, expires_at)
			values ($1, 1, 'o', 'a/b', 'branch', 'main', repeat('a', 40), 'img', 'web-app', '0.1.0', 'S', 'c', 't', 'tid', now())`, id)
		if sqlState(err) != "23514" {
			t.Errorf("id %q: SQLSTATE %q, want 23514 (CHECK violation)", id, sqlState(err))
		}
	}
}
