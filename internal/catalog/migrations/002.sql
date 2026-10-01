ALTER TABLE assets ADD COLUMN catalog_number INTEGER CHECK(catalog_number > 0);
UPDATE assets SET catalog_number=id;
CREATE UNIQUE INDEX assets_catalog_number ON assets(catalog_number);
CREATE INDEX assets_catalog_order ON assets(archived, catalog_number DESC);
