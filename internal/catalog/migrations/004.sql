-- Current visible catalog numbers become the sole asset IDs. Temporary negative
-- IDs make arbitrary permutations collision-free inside the migration transaction.
UPDATE assets SET id=-catalog_number;
UPDATE assets SET id=-id;
UPDATE sqlite_sequence SET seq=max(seq,COALESCE((SELECT max(id) FROM assets),0)) WHERE name='assets';
DROP INDEX assets_catalog_number;
DROP INDEX assets_catalog_order;
ALTER TABLE assets DROP COLUMN catalog_number;
