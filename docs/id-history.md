# ID-change history

SQLite schema 5 adds `asset_id_changes`. It records one event for each successful
Syscat ID move or swap, in the same transaction as the record updates and ID
allocation. If writing history fails, the move or swap rolls back too. Rejected
requests and no-ops add no events. Migration creates an empty history table;
changes made before this feature are not reconstructed.

Each event includes:

- `event_id`: sequential history event number, independent of asset IDs.
- `occurred_at`: UTC timestamp in RFC 3339 format.
- `origin`: `browser`, `api`, or `internal`.
- `old_id` and `new_id`: source ID before and after the change.
- `source_key`, `source_title`, `source_revision`, `source_new_revision`: private
  intake key, full short description at the time, and source revision transition.
- `swapped_key`, `swapped_title`, `swapped_revision`, `swapped_new_revision`:
  corresponding information for an occupied target. All are NULL for a move.
  For a swap, the occupant moves from `new_id` to `old_id`.

The keys reuse the existing unique `assets.submission_key`. They are not another
public asset identifier, and history is not exposed in normal API responses or
inventory exports. They stay with the asset through ID changes, allowing backend
queries to find its current ID even after repeated moves and swaps. Snapshotted
titles and revisions stay as recorded when the asset is edited later. Syscat
appends events and does not rewrite previous ones.

To inspect events in order:

```sql
SELECT event_id, occurred_at, origin,
       old_id, new_id, source_title,
       source_revision, source_new_revision,
       swapped_title, swapped_revision, swapped_new_revision
FROM asset_id_changes
ORDER BY event_id;
```

To connect historic participants with their current records:

```sql
SELECT h.event_id, h.occurred_at, h.old_id, h.new_id,
       h.source_title, source.id AS source_current_id,
       h.swapped_title, occupant.id AS occupant_current_id
FROM asset_id_changes AS h
LEFT JOIN assets AS source ON source.submission_key = h.source_key
LEFT JOIN assets AS occupant ON occupant.submission_key = h.swapped_key
ORDER BY h.event_id;
```

For all events involving the asset currently at ID 15:

```sql
SELECT h.*
FROM asset_id_changes AS h
JOIN assets AS a
  ON a.submission_key = h.source_key OR a.submission_key = h.swapped_key
WHERE a.id = 15
ORDER BY h.event_id;
```

History records committed application operations, not direct edits made outside
Syscat. `origin` describes the entry point, not a person's identity: trusted-network
access currently has no authenticated actor. Future authentication can add actor
metadata without changing the asset numbering model. History does not restore
broken links or redirect old IDs.
