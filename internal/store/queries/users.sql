-- name: UpsertUser :one
insert into users (github_id, login, avatar_url)
values ($1, $2, $3)
on conflict (github_id) do update
    set login = excluded.login,
        avatar_url = excluded.avatar_url,
        last_login_at = now()
returning *;

-- name: GetUserByGithubID :one
select * from users where github_id = $1;
