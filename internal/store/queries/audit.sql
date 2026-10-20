-- name: InsertAuditEvent :one
insert into audit_events (actor_github_id, actor_login, action, environment_id, outcome, reason, trace_id)
values ($1, $2, $3, $4, $5, $6, $7)
returning id, occurred_at;

-- name: ListAuditEvents :many
-- Filters are optional (NULL = no filter). The cursor is the (occurred_at, id)
-- of the last row of the previous page; row-value comparison keeps paging
-- stable when several events share a timestamp.
select a.id, a.occurred_at, a.actor_github_id, a.actor_login, a.action,
       a.environment_id, a.outcome, a.reason, a.trace_id,
       coalesce(u.avatar_url, '')::text as actor_avatar_url
from audit_events a
left join users u on u.github_id = a.actor_github_id
where (sqlc.narg('outcome')::text is null or a.outcome = sqlc.narg('outcome')::text)
  and (sqlc.narg('action')::text is null or a.action = sqlc.narg('action')::text)
  and (sqlc.narg('actor')::text is null or a.actor_login = sqlc.narg('actor')::text)
  and (sqlc.narg('cursor_at')::timestamptz is null
       or (a.occurred_at, a.id) < (sqlc.narg('cursor_at')::timestamptz, sqlc.narg('cursor_id')::uuid))
order by a.occurred_at desc, a.id desc
limit sqlc.arg('row_limit')::int;
