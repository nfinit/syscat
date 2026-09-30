# Changelog

## Unreleased

## 1.6.0 - 2026-09-30

- Search current photo captions alongside descriptions, locations, and inventory
  IDs in the collection and read-only API.
- Remember photo group collapse states per entry in browser storage across
  refreshes, including archived views; retain collapse behavior without storage.
- Start existing entries with photo editing controls hidden behind Edit photos,
  retaining editable text fields, pending photo changes, and plain HTML controls.
  Open photo editing after validation errors.
- Remove the redundant Add more photos control and additional upload fields;
  retain multi-file selection and simplify the Add photos label.

No SQLite schema migration is required.

## 1.5.4 - 2026-09-30

- Mark attached photos for deletion on save, with an undo control and plain HTML
  checkboxes. Promote a remaining photo when deleting the overview; require at
  least one photo. Remove originals, thumbnails, and intake photo references on
  deletion, preserving all other intake fields.

No SQLite schema migration is required.

## 1.5.3 - 2026-09-30

- Place compact group-order controls beside the section title.
- Separate existing photo groups from the upload area with a divider.
- Place photo movement and overview actions in the associated caption bar.
- Finish caption and group edits with Enter without submitting the entire form
  or returning to the top of the page.

## 1.5.2 - 2026-09-30

- Organize photos into named groups with section and photo ordering, a dedicated
  overview, and an ungrouped Photos section. JavaScript adds collapse controls;
  plain HTML displays all sections. Include group names in the API and JSON export.
- Keep photos and focus in place while entering group names; saving applies
  group assignments.
- Reveal group editors on demand with Edit group controls beside image and caption
  controls, using section headings for group names; plain HTML keeps
  the fields visible.

No SQLite schema migration is required. Group names are additive fields in
JSON exports and API responses; CSV columns remain unchanged.

## 1.5.1 - 2026-09-30

- Arrange attached photos before saving with move controls, or numbered positions
  in plain HTML. The first photo becomes overview; captions and intake are preserved.
- Remove the redundant overview selection controls; saved photo order determines
  the overview.
- Simplify existing photo headings and match caption controls to full-image links.

No SQLite schema or export format changes.

## 0.5.0 - 2026-09-30

- Mark an existing or newly selected photo as overview, moving it first while
  retaining every attached photo and its caption. New uploads can be selected
  before saving; original intake remains unchanged.

No SQLite schema or export format changes.

## 0.4.0 - 2026-09-30

- Add optional photo captions during upload and editing, including archived
  photo display and read-only API metadata. Existing photos need no migration.
- Display captions inline with full-image links; JavaScript reveals caption
  fields on demand, while plain HTML keeps the fields visible.

No SQLite schema migration is required. Photo captions are additive fields in
JSON exports and API responses; CSV columns remain unchanged.

## 0.3.1 - 2026-09-30

- Make the API discoverable with a JSON index at `/api` and `/api/`, an API link
  in the site footer, and a `service-desc` Link header pointing to OpenAPI.

## 0.3.0 - 2026-09-30

- Add a read-only JSON API for paginated inventory search and individual records,
  including original intake and URLs for overview/detail photos and thumbnails.
- Provide structured API errors, strict listing parameters, and an embedded
  OpenAPI description at `/api/openapi.json`. API access requires no session.

No SQLite schema or export format changes.

## 0.2.0 - 2026-09-30

- Require an overview photo and description on intake and edit forms. Existing
  photos satisfy the overview requirement; location and detail photos stay optional.
  Server validation enforces the requirements without JavaScript or browser validation.
- Simplify the application header by removing the S icon before the Syscat title.
- Remove redundant description and collection-photo guidance from entry forms.

Breaking intake change: saves now reject missing overview photos or blank
descriptions, which were previously accepted. Older entries remain readable;
entries without photos must attach an overview before saving edits. Original
intake is preserved. New overview uploads use the `overview` form field;
`photos` holds optional detail uploads.

No SQLite schema or export format changes.

## 0.1.1 - 2026-09-30

- Raise the default total upload request limit from 32 to 256 MiB and add
  `--max-upload-mib` configuration. Display the configured limit on forms and
  in upload errors; retain the 12 MiB / 32 megapixel per-image limits.
- Allow up to 15 minutes for requests and responses during larger uploads.

No SQLite schema or export format changes.

## 0.1.0 - 2026-09-30

- Establish Syscat phase 0 from Cardcat 0.2.0, with independent branding,
  executable, module, sessions, exports, and `syscat.sqlite3` storage.
- Default to all network interfaces on port 8800 and print local access URLs
  at startup. Explicit `--listen` bindings remain supported.
- Assign permanent inventory numbers displayed without prefixes.
- Replace card classifications and separate notes with location and a detailed,
  multiline description. Preserve original intake when descriptions change.
- Support an overview and open-ended detail/followup photos, multiple selection,
  optional extra upload inputs, and previews for every selected image.
- Retain duplicate-save protection, revision conflicts, archive/restore,
  photo validation and rollback, search, exports, and the build identity helper.

Syscat starts a new schema at version 1 and a new application version at 0.1.0.
Cardcat inventory import is deferred; its database is not a Syscat database.
