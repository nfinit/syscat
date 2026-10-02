-- One append-only application event per successful ID move or swap. Intake keys
-- already identify records privately and stay unchanged when their public IDs move.
CREATE TABLE asset_id_changes (
    event_id INTEGER PRIMARY KEY AUTOINCREMENT,
    occurred_at TEXT NOT NULL,
    origin TEXT NOT NULL CHECK(origin IN ('browser','api','internal')),
    old_id INTEGER NOT NULL CHECK(old_id > 0),
    new_id INTEGER NOT NULL CHECK(new_id > 0 AND new_id != old_id),
    source_key TEXT NOT NULL,
    source_title TEXT NOT NULL,
    source_revision INTEGER NOT NULL,
    source_new_revision INTEGER NOT NULL,
    swapped_key TEXT,
    swapped_title TEXT,
    swapped_revision INTEGER,
    swapped_new_revision INTEGER,
    CHECK ((swapped_key IS NULL AND swapped_title IS NULL AND swapped_revision IS NULL AND swapped_new_revision IS NULL)
        OR (swapped_key IS NOT NULL AND swapped_title IS NOT NULL AND swapped_revision IS NOT NULL AND swapped_new_revision IS NOT NULL))
);
CREATE INDEX asset_id_changes_source ON asset_id_changes(source_key,event_id);
CREATE INDEX asset_id_changes_swapped ON asset_id_changes(swapped_key,event_id);
