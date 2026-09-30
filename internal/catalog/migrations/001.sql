CREATE TABLE assets (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    description TEXT NOT NULL DEFAULT '',
    location TEXT NOT NULL DEFAULT '',
    photos TEXT NOT NULL DEFAULT '[]',
    intake TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    revision INTEGER NOT NULL DEFAULT 1,
    archived INTEGER NOT NULL DEFAULT 0,
    submission_key TEXT NOT NULL UNIQUE
);
CREATE INDEX assets_recent ON assets(archived, id DESC);
