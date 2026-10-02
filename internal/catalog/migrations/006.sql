-- Intake allocation is independent of mutable asset IDs and SQLite's rowid
-- high-water mark. Recover original intake numbers where recorded, or the ID
-- before the earliest recorded move/swap. Older records with neither fall back
-- to their current ID; their earlier numbering cannot be reconstructed.
CREATE TABLE asset_id_allocator (
    singleton INTEGER PRIMARY KEY CHECK(singleton=1),
    next_id INTEGER NOT NULL CHECK(typeof(next_id)='integer' AND next_id>0)
);
WITH original_ids AS (
    SELECT COALESCE(
        CASE WHEN json_type(intake,'$.catalog_number')='integer'
            AND json_extract(intake,'$.catalog_number')>0
            THEN json_extract(intake,'$.catalog_number') END,
        CASE WHEN json_type(intake,'$.id')='integer'
            AND json_extract(intake,'$.id')>0
            THEN json_extract(intake,'$.id') END,
        (SELECT prior_id FROM (
            SELECT event_id,old_id AS prior_id FROM asset_id_changes WHERE source_key=assets.submission_key
            UNION ALL
            SELECT event_id,new_id AS prior_id FROM asset_id_changes WHERE swapped_key=assets.submission_key
        ) ORDER BY event_id LIMIT 1),
        id
    ) AS original_id FROM assets
), highest AS (SELECT COALESCE(max(original_id),0) AS id FROM original_ids)
INSERT INTO asset_id_allocator(singleton,next_id)
SELECT 1,CASE WHEN id=9223372036854775807 THEN id ELSE id+1 END FROM highest;
