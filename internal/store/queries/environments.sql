-- name: InsertEnvironment :one
insert into environments (
    id, owner_github_id, owner_login, repo, ref_type, ref, sha, image,
    template, chart_version, size, "values", correlation_id, traceparent, trace_id, expires_at
) values (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16
)
returning *;

-- name: GetEnvironment :one
select * from environments where id = $1;

-- name: ListEnvironmentsByOwner :many
select * from environments
where owner_github_id = $1
order by created_at desc, id desc
limit $2;

-- name: ListActiveEnvironments :many
-- Everything the sweeper must keep moving: not yet running and not failed.
select * from environments
where status in ('requested', 'provisioning')
order by created_at;

-- name: ClaimDispatch :execrows
-- Compare-and-set: only the first caller sees 1 row. A second caller (retry,
-- sweeper) sees 0 and must NOT post a second workflow dispatch.
update environments
set dispatch_attempted_at = now(), updated_at = now()
where id = $1 and status = 'requested' and dispatch_attempted_at is null;

-- name: MarkProvisioning :execrows
update environments
set status = 'provisioning', run_id = $2, run_html_url = $3,
    dispatched_at = now(), provision_deadline_at = $4, updated_at = now()
where id = $1 and status = 'requested';

-- name: UpdateRunETag :exec
update environments set run_etag = $2, updated_at = now() where id = $1;

-- name: MarkRunConcluded :execrows
-- Set once: the first poller to see the conclusion wins and emits the spans.
update environments
set run_conclusion = $2, run_concluded_at = now(), updated_at = now()
where id = $1 and run_concluded_at is null;

-- name: MarkRunning :execrows
update environments
set status = 'running', running_at = now(), updated_at = now()
where id = $1 and status = 'provisioning';

-- name: MarkFailed :execrows
update environments
set status = 'failed', failure_reason = $2, updated_at = now()
where id = $1 and status in ('requested', 'provisioning');
