-- +goose Up

-- The runtime role. The compose/owner user keeps migrating (MIGRATE_DATABASE_URL);
-- the API and worker connect as ephematool_app (DATABASE_URL) with only the
-- grants below (D-24). The password is DEV-ONLY: outside local dev, create the
-- role with a real secret and run ALTER ROLE out of band; this block then skips.
-- +goose StatementBegin
DO $$
BEGIN
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'ephematool_app') THEN
    CREATE ROLE ephematool_app LOGIN PASSWORD 'ephematool_app';
  END IF;
END
$$;
-- +goose StatementEnd

GRANT USAGE ON SCHEMA public TO ephematool_app;
GRANT SELECT, INSERT, UPDATE ON users TO ephematool_app;

create table environments (
    id                    text primary key check (id ~ '^pv-[a-z0-9]{10}$'),
    owner_github_id       bigint not null,
    owner_login           text not null,
    repo                  text not null,
    ref_type              text not null check (ref_type in ('branch', 'pr', 'sha')),
    ref                   text not null,
    sha                   text not null check (sha ~ '^[0-9a-f]{40}$'),
    image                 text not null,
    template              text not null,
    chart_version         text not null,
    size                  text not null check (size in ('S', 'M', 'L')),
    "values"              jsonb not null default '{}',
    -- Phase 3 adds 'destroying' and 'destroyed' by replacing this CHECK in one migration.
    status                text not null default 'requested'
                          check (status in ('requested', 'provisioning', 'running', 'failed')),
    failure_reason        text not null default '',
    correlation_id        text not null,
    traceparent           text not null,
    trace_id              text not null,
    run_id                bigint,
    run_html_url          text not null default '',
    run_etag              text not null default '',
    run_conclusion        text not null default '',
    run_concluded_at      timestamptz,
    dispatch_attempted_at timestamptz,
    dispatched_at         timestamptz,
    provision_deadline_at timestamptz,
    running_at            timestamptz,
    created_at            timestamptz not null default now(),
    updated_at            timestamptz not null default now(),
    expires_at            timestamptz not null
);

create index environments_owner_created_idx on environments (owner_github_id, created_at desc);
-- The sweeper scans only unfinished rows, so keep that index tiny.
create index environments_unfinished_idx on environments (created_at)
    where status in ('requested', 'provisioning');

REVOKE ALL ON environments FROM PUBLIC;
GRANT SELECT, INSERT, UPDATE ON environments TO ephematool_app;

-- +goose Down
drop table environments;
REVOKE SELECT, INSERT, UPDATE ON users FROM ephematool_app;
-- The role itself is kept: other databases or sessions may still reference it.
