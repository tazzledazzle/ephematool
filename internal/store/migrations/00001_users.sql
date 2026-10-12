-- +goose Up
create table users (
    github_id     bigint primary key,
    login         text not null,
    avatar_url    text not null default '',
    first_login_at timestamptz not null default now(),
    last_login_at  timestamptz not null default now()
);

-- +goose Down
drop table users;
