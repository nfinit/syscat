-- Schema 3 already split all legacy descriptions into the independent fields.
-- The combined API/export value is now derived, never separately persisted.
-- Original intake snapshots remain untouched, including their legacy text.
ALTER TABLE assets DROP COLUMN description;
