-- Schema for City Service Bot
create table if not exists users (
    id bigserial primary key,
    tg_user_id bigint unique not null,
    username text,
    first_name text,
    last_name text,
    is_admin boolean not null default false,
    created_at timestamptz not null default now()
);

create table if not exists chats (
    chat_id bigint primary key,
    type text not null,
    title text,
    created_at timestamptz not null default now()
);

create table if not exists issues (
    id bigserial primary key,
    user_id bigint not null references users(id) on delete cascade,
    chat_id bigint not null references chats(chat_id) on delete cascade,
    text text,
    latitude double precision,
    longitude double precision,
    status text not null default 'Новая',
    district text,          -- 👈 новый столбец
    category text,          -- 👈 новый столбец
    created_at timestamptz not null default now(),
    updated_at timestamptz not null default now()
);

create index if not exists idx_issues_status on issues(status);
create index if not exists idx_issues_created_at on issues(created_at);

create table if not exists attachments (
    id bigserial primary key,
    issue_id bigint not null references issues(id) on delete cascade,
    file_id text not null,          -- Telegram file_id
    file_type text not null,        -- photo, video, document
    local_path text not null,       -- путь к файлу в uploads/
    created_at timestamptz not null default now()
);

create table if not exists status_changes (
    id bigserial primary key,
    issue_id bigint not null references issues(id) on delete cascade,
    old_status text,
    new_status text not null,
    changed_by bigint references users(id),
    comment text,
    created_at timestamptz not null default now()
);

create table if not exists comments (
    id bigserial primary key,
    issue_id bigint not null references issues(id) on delete cascade,
    admin_user_id bigint not null references users(id) on delete cascade,
    text text not null,
    created_at timestamptz not null default now()
);

create table if not exists broadcasts (
    id bigserial primary key,
    text text not null,
    created_by bigint references users(id),
    sent_count int not null default 0,
    created_at timestamptz not null default now()
);
