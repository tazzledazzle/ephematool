-- +goose Up
-- Append-only audit log (D-19, D-24). The app role gets SELECT and INSERT only.
-- A row trigger blocks UPDATE/DELETE and a statement trigger blocks TRUNCATE,
-- even for the table owner. The owner *can* still ALTER TABLE ... DISABLE
-- TRIGGER, so the guarantee is "no application code path", not "DBA-proof".
-- Never use ALTER DEFAULT PRIVILEGES in this project: it would hand UPDATE on
-- audit_events to the app role.
create table audit_events (
    id              uuid primary key default gen_random_uuid(),
    occurred_at     timestamptz not null default now(),
    actor_github_id bigint not null,
    actor_login     text not null,
    action          text not null,
    environment_id  text,
    outcome         text not null check (outcome in ('success', 'denied', 'rejected')),
    -- A stable machine code (not UI copy), for example not_creator.
    reason          text not null default '',
    trace_id        text not null default ''
);

-- Keyset paging: order by (occurred_at, id) descending.
create index audit_events_occurred_idx on audit_events (occurred_at desc, id desc);

-- +goose StatementBegin
CREATE FUNCTION audit_events_immutable() RETURNS trigger LANGUAGE plpgsql AS $f$
BEGIN
  RAISE EXCEPTION 'audit_events is append-only' USING ERRCODE = 'restrict_violation';
END
$f$;
-- +goose StatementEnd

CREATE TRIGGER audit_events_no_mutation BEFORE UPDATE OR DELETE ON audit_events
    FOR EACH ROW EXECUTE FUNCTION audit_events_immutable();
CREATE TRIGGER audit_events_no_truncate BEFORE TRUNCATE ON audit_events
    FOR EACH STATEMENT EXECUTE FUNCTION audit_events_immutable();

REVOKE ALL ON audit_events FROM PUBLIC;
GRANT SELECT, INSERT ON audit_events TO ephematool_app;

-- +goose Down
-- Dropping the table drops its triggers; the function goes with an explicit drop.
drop table audit_events;
drop function audit_events_immutable();
