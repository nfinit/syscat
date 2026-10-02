# Inventory API

The API is served by the same Syscat executable on the same port as the web UI.
It requires no browser session, cookies, or CSRF token. Like the existing catalog,
it is accessible to anyone who can reach this trusted-network server.

| Endpoint | Purpose |
| --- | --- |
| `GET /api` | API discovery index (`/api/` is also supported) |
| `GET /api/assets` | Paginated listing and search |
| `POST /api/assets` | Create a record with overview and optional detail photos |
| `GET /api/assets/{id}` | Full current record and original intake |
| `PATCH /api/assets/{id}` | Update current short description, details, or location |
| `PATCH /api/assets/{id}/photos/{photo_id}` | Update a current photo caption or group |
| `POST /api/assets/{id}/photos` | Attach one or more photos to an existing asset |
| `POST /api/assets/{id}/id` | Move or swap an asset ID (breaks links) |
| `GET /api/openapi.json` | Embedded OpenAPI 3.0.3 description |

Read endpoints support GET and HEAD; text edits use PATCH; creation, uploads, and catalog-number changes use POST.
Unsupported methods return HTTP 405 with the endpoint’s allowed methods. Unknown
API endpoints return HTTP 404. Responses and errors are JSON with
`Cache-Control: no-store`. API calls do not create sessions. Photo deletion/ordering and archive operations
remain web-interface features.

## Discovery

Starting from the server address, clients can discover the API through the
`Link` response header on pages, redirects, API responses, and errors:

```http
Link: </api/openapi.json>; rel="service-desc"; type="application/json"
```

The `service-desc` relation identifies a machine-readable service description
([RFC 8631](https://www.rfc-editor.org/rfc/rfc8631)). The site footer also links
to `/api`. Both `/api` and `/api/` return a JSON index with `name`, `build`,
`read_only: false`, `write_authentication: "trusted_network"`, and a `links` object
pointing to `self`, `assets`, `summaries`, and `openapi`.
All links are root-relative and work without cookies.

```sh
curl -I http://127.0.0.1:8800/
curl http://127.0.0.1:8800/api
```

## Listing and search

```sh
curl http://127.0.0.1:8800/api/assets
curl 'http://127.0.0.1:8800/api/assets?q=00001&page_size=10'
curl 'http://127.0.0.1:8800/api/assets?archived=1'
curl 'http://127.0.0.1:8800/api/assets?view=summary&page_size=100'
curl 'http://127.0.0.1:8800/api/assets?view=summary&field=title&q=Apple'
curl 'http://127.0.0.1:8800/api/assets?field=caption&q=unknown+chip'
```

| Parameter | Default | Meaning |
| --- | --- | --- |
| `q` | Empty | Description/location/photo-caption substring search, or exact asset ID |
| `field` | `all` | Restrict `q` to `all`, `title`, `description`, `location`, `caption`, `id`, `catalog_number`, `short_description`, or `details` |
| `page` | `1` | Page number, from 1 to 1,000,000 |
| `page_size` | `50` | Entries per page, from 1 to 100 |
| `archived` | `0` | `0`/`false` for active entries; `1`/`true` for archived entries |
| `view` | `full` | `full` for current record metadata; `summary` for compact catalog review |

Search uses the same behavior as the HTML catalog: case-insensitive text matching
under SQLite's existing LIKE rules, literal `%` and `_`, and exact numeric IDs
with or without zero padding. Caption matching searches current photos only; an
entry appears once even if multiple captions match. Results are ordered by
descending current asset ID.
`field=title` and `field=short_description` search the complete short description,
including text beyond the display title's truncation limit. `field=details`
searches longer notes only. `field=description` searches the legacy combined
projection, including the short description and details. `field=location` searches only
location; `field=caption` searches current photo captions; `field=id` matches only
an exact positive current asset ID (zero padding is allowed).
`field=catalog_number` is a compatibility alias for the same ID. Broad search
matches either numeric identity; use an explicit field to disambiguate a swap. A nonnumeric
ID query matches nothing. Original intake and photo group names are excluded.
An omitted field or `field=all` keeps the existing broad search. Empty `q` lists
all entries in the selected archive set, regardless of field. Field names must
match these lowercase values exactly; empty or invalid selectors return 400.

Active and archived records are separate sets. Unknown, repeated, malformed, or
out-of-range parameters return HTTP 400. Empty `archived` is treated as false.

The response contains `assets`, `total`, `page`, `page_size`, `query`, and
`archived`. Root-relative `previous` and `next` URLs appear when another page is
available in that direction and retain the search, archive filter, and explicit
view and field selections. The envelope includes `field` only when explicitly
requested. An empty result uses `"assets": []`. Full lists include current
descriptions and photo metadata; original-intake snapshots are available only on
individual records. Omitting `view` preserves the original full listing format.

### Summary listings

For a large catalog, begin with `GET /api/assets?view=summary&page_size=100`.
The envelope also includes `"view": "summary"`; each asset contains only:

- Current `id`, compatibility alias `catalog_number`, display `label`, and `title`.
- `revision`, `photo_count` (current attached photos), and `archived`.
- `url` for the web page and `api_url` for the full record and asset PATCH endpoint.

Summaries omit description bodies, locations, timestamps, photo metadata, and
intake snapshots. Search defaults to descriptions, locations, and captions; `field` can restrict it.
Follow an asset's `api_url` to read full context before editing, including any
description lines beyond its display title. Pagination links retain `view=summary`.
The `/api` index advertises this starting point through `links.summaries`.

## Individual records and images

```sh
curl http://127.0.0.1:8800/api/assets/1
curl http://127.0.0.1:8800/api/assets/00001
```

Each record contains numeric `id`, its compatibility alias `catalog_number`, display
`label`, a short `title`, the full
`short_description`, `details`, legacy combined `description`, `location`, ordered `photos`, `created_at`, `updated_at`,
`revision`, and `archived`. It also provides `url` for the HTML entry and
`api_url` for its JSON representation, `photo_upload_url` for attaching photos,
and `id_change_url` for renumbering (`catalog_number_url` is a legacy alias).
URLs use the current `id`; moving/swapping an ID breaks existing asset links.
Individual records include
`original_intake`, the stored initial snapshot. Archived records are readable
by ID. An invalid ID returns 400; an ID with no record returns 404.

Each current photo provides:

- `id`: stable opaque photo ID, unchanged by ordering, captions, groups, or overview selection.
- `api_url`: the photo’s PATCH endpoint (it returns the updated full asset).

- `original_name`: the uploaded filename.
- `group`: optional group name (up to 100 characters), empty for overview and ungrouped photos.
  Named groups are contiguous in saved order, followed by ungrouped detail photos.
- `caption`: optional descriptive text, or an empty string when absent (up to 1,000 characters).
- `role`: `overview` for the first current photo, `detail` for subsequent photos.
  Photos follow the saved display order. The first photo is the overview.
- `original_url`: the uploaded image with location-capable metadata removed, without recompressing image data.
- `thumbnail_url`: the orientation-corrected JPEG display image.

URLs begin with `/` and should be resolved against the server origin used for
the API request. For example, `/photos/example.jpg` from a request to
`http://10.0.0.81:8800/api/assets/1` is retrieved from
`http://10.0.0.81:8800/photos/example.jpg`. No image bytes are embedded in JSON.
Retrieve thumbnails for quick visual inspection and originals for small markings.
New uploads have location-capable metadata removed from the served originals;
JPEG orientation is retained. Existing images are unchanged by this upload policy.

Original intake is returned as stored, with photo `path` and `thumbnail` values
instead of URL fields. Prefix those paths with `/` to retrieve the images from
the same server. Its snapshot ID may be `0` because it was captured before the
sequential ID was allocated; use the outer record's `id` to address the entry.
Older intake snapshots may omit `catalog_number`. New intake captures its initial
catalog number; renumbering never rewrites either kind of snapshot.
Deleting a photo removes it from current `photos` and any original-intake photo
references. Its original and thumbnail files are deleted after a successful save.
Other original-intake fields remain unchanged.

## Editing records

Writes currently use trusted-network access without credentials. All write routes
pass through a shared authorization policy so authentication can be added later.
Browser form sessions and CSRF handling are independent of these API endpoints.

Read the asset first and include its current positive integer `revision` in every
PATCH or attachment upload. Creation allocates a new record at revision 1 and
does not accept a revision. Each successful write returns the full updated asset, including its new
revision and original intake. A concurrent or stale edit returns HTTP 409 with
`error.code: "conflict"`; reread the asset and reconcile before retrying. Archived
entries cannot be edited through the API; restore them in the web interface first.

For asset text, supply `short_description`, `details`, `location`, or any
combination. Omitted fields remain unchanged, so a title-only edit preserves all
longer notes:

```sh
curl -X PATCH http://127.0.0.1:8800/api/assets/1 \
  -H 'Content-Type: application/json' \
  -d '{"revision":7,"short_description":"IBM RS/6000 43P Model 150"}'
```

For a photo, follow its `api_url` or use its `id`:

```sh
curl -X PATCH http://127.0.0.1:8800/api/assets/1/photos/PHOTO_ID \
  -H 'Content-Type: application/json' \
  -d '{"revision":8,"caption":"SMSC Super I/O controller","group":"Motherboard"}'
```

These example revisions must be replaced with the asset’s actual current revision.
Revision belongs to the whole asset, including photo edits. Photo PATCH returns
an asset, not a standalone photo: locate the photo by its stable `id` in the result.

Omitted fields remain unchanged. Short descriptions and other metadata text are
trimmed; details preserve whitespace and paragraph formatting. An empty string clears location,
caption, group, or details. Short description must remain nonempty and single-line. Setting a detail photo’s group
normalizes section ordering while preserving existing group order where possible;
the overview cannot join a named group. IDs, paths, image bytes, intake, timestamps,
and archive state cannot be supplied as editable fields.

Bodies must be a single JSON object with `Content-Type: application/json`, limited
to 128 KiB, with revision and at least one editable field. Null values, unknown or
duplicate fields, invalid types, and query parameters are rejected. Limits match
the web form: short description and details 20,000 UTF-8 bytes each, location 500 UTF-8 bytes, caption
1,000 characters, and single-line group names 100 characters.

## Creating records

POST multipart form data to `/api/assets`, with a nonempty single-line `short_description` and
exactly one `overview` file. `details`, `location`, `caption_overview`, and any number of
`photos` detail files are optional. Short description is trimmed; details preserve their exact text. Both are limited
to 20,000 UTF-8 bytes each; short description supplies the title. Location is trimmed and
limited to 500 UTF-8 bytes. Each scalar field must appear at most once.
The overview is always ungrouped. Detail `caption_photos` and `group_photos`
follow the same file-order/count rules as attachment uploads below. Captions
are limited to 1,000 characters and single-line groups to 100 characters.

```sh
curl http://127.0.0.1:8800/api/assets \
  -H 'Idempotency-Key: recycler-intake-2026-09-30-example' \
  --form-string 'short_description=Unidentified desktop system' \
  --form-string 'details=Acquired as photographed; configuration to investigate.' \
  --form-string 'location=Workbench' \
  -F 'overview=@system.jpg' \
  --form-string 'caption_overview=As acquired' \
  -F 'photos=@motherboard.jpg' \
  --form-string 'caption_photos=Motherboard markings' \
  --form-string 'group_photos=Interior'
```

Creation uses the same image formats, metadata stripping, thumbnails, and upload
limits as attachments. HTTP 201 returns the full asset with its current ID,
photo IDs/links, revision 1, and `original_intake`. The `Location` header points
to the new full-record endpoint. All submitted observations and the initial
photo collection are captured in intake, just as with browser intake. Subsequent
PATCHes and uploads can use the returned revision and links immediately.
A failed creation leaves no record or generated images.

Use an `Idempotency-Key` when retrying may be necessary. It is optional,
case-sensitive, and must appear once with 1–128 ASCII letters, digits, dots,
underscores, or hyphens. Generate a unique key for each logical intake and retain
it for retries. The first successful creation owns that key permanently, even
across restarts or archiving. Replaying a valid multipart creation request with
the same key returns HTTP 200 and that record's **current** state, preserving all
later edits and original intake, without creating more records or images.
Replacement content is ignored after basic request validation; replay is not an
update operation. Use a fresh key to create a different system. Failed requests
do not reserve their key. Without a key, every successful request creates a new
record, including repeated requests with identical content.

Unknown fields, duplicated scalar fields, query parameters, and invalid intake
return HTTP 400. Creation does not accept `id`, `revision`, archive state, intake,
or image paths. The server assigns those values. Authorization passes through
the shared API write policy before parsing the request.

## Attaching photos

Follow the full record's `photo_upload_url`, or POST to `/api/assets/{id}/photos`.
Use `multipart/form-data` with file bytes rather than base64 or remote image URLs:

```sh
curl http://127.0.0.1:8800/api/assets/1/photos \
  -F 'revision=9' \
  -F 'photos=@exterior.jpg' \
  -F 'photos=@motherboard.jpg' \
  --form-string 'caption_photos=Rear connectors' \
  --form-string 'caption_photos=Motherboard overview' \
  --form-string 'group_photos=Exterior' \
  --form-string 'group_photos=Interior'
```

Replace the example revision with the current asset revision. `revision` must
appear exactly once, and at least one `photos` file is required. For multiple
files, repeat `photos`; its multipart order defines the uploaded file order.
`caption_photos` and `group_photos` are each optional: omit the field entirely,
or provide exactly one value per file in the same order, using empty strings for
missing values. Captions and groups use the same trimming and limits as PATCH.
Unknown fields, misplaced file parts, repeated revisions, mismatched metadata
counts, and query parameters return HTTP 400.

Files use the same pipeline as browser uploads: JPEG, PNG, or GIF; at most
12 MiB per photo and 32 megapixels; total request size limited by
`--max-upload-mib` (256 MiB by default). Location-capable metadata is stripped,
JPEG orientation retained, and display thumbnails generated. Oversized files or
requests return HTTP 413; unsupported/invalid images return HTTP 400; an incorrect
request media type returns HTTP 415.

A successful batch returns HTTP 201, a `Location` header pointing to the full
record, and the full updated asset including stable photo IDs and its new
revision. Photos append within their groups, keeping the existing overview and
named group order; new named groups follow in upload order, with ungrouped details
last. For a legacy record without photos, the first attachment becomes the
overview and must have an empty group. Existing descriptions, locations, and
original intake remain unchanged. Later attachments are not original intake.

The batch saves as one revision. Invalid files or a failed save reject the whole
batch and remove its generated originals and thumbnails. Concurrent edits or
archived records return HTTP 409. If a response is lost, reread the asset and
check which photos were attached before retrying; do not blindly repeat an upload
with a newer revision, which would attach duplicates. Authentication can gate
this route through the same write policy used for PATCH.

## Asset ID changes and breaking links

`id` is the sole sequential asset identifier, used in `label` and all asset URLs.
The deprecated `catalog_number` field mirrors it for early clients; there is no
separate stored catalog number. New intake follows an independent sequential
counter, skipping occupied IDs, including archived assets. Renumbering does not
advance or rewind this counter, so high vanity IDs do not leave large gaps in
normal intake. A skipped occupied ID is not revisited automatically if later freed.

**Renumbering is deliberately breaking.** Moving to an unused ID makes the old
asset and API URLs return 404. Swapping IDs makes each old URL identify the other
asset. Bookmarks, external references, and API photo-metadata URLs must be updated.
Original image and thumbnail URLs, photo IDs, observations, and original intake
stay with their records. Re-fetch records after a change; a remembered URL is no
longer a permanent identity, and a revision alone cannot identify a different
asset that later occupies the same ID.

To move a record, POST to its `id_change_url` with explicit acknowledgment:

```sh
curl -X POST http://127.0.0.1:8800/api/assets/43/id \
  -H 'Content-Type: application/json' \
  -d '{"revision":6,"id":100,"acknowledge_link_changes":true}'
```

For an occupied target, read both records (the target may be archived) and supply
its current ID and revision:

```sh
curl -X POST http://127.0.0.1:8800/api/assets/43/id \
  -H 'Content-Type: application/json' \
  -d '{"revision":6,"id":1,"swap_id":1,"swap_revision":1,"acknowledge_link_changes":true}'
```

The source takes the requested ID; the occupant takes the source's former ID.
HTTP 200 returns `asset` and, for a swap, `swapped_asset`, with their **new** URLs.
The `Location` header identifies the source's new API URL. Both revisions advance
on a swap; only the source advances on a move. Requesting the current ID without
swap fields is a no-op. The operation is transactional, including ID allocation and a persistent ID-change
history event. Failed changes and no-ops create no event. Backend history includes
the API/browser/internal origin and private intake keys; it is not added to normal
responses. See [ID-change history](id-history.md) for SQL analysis examples.

`acknowledge_link_changes` must be boolean `true`. Occupied targets require both
`swap_id` and `swap_revision`; omit both for a free target. Missing/wrong/stale
swap expectations or changed occupancy return 409 without partial changes. A
vacated source URL returns 404. Archived sources must be restored first; archived
occupants may participate in an explicit swap and remain archived. The shared
authorization policy, strict JSON parsing, 128 KiB limit, and revision checks apply.
Normal PATCH/creation do not accept an editable ID.

For compatibility, `/api/assets/{id}/catalog-number` remains a route alias, and
`catalog_number` is accepted instead of `id` in the request (never both). These
aliases have the same breaking behavior and acknowledgment requirement.

The browser's **Change ID** control previews the affected assets and the broken
links, then requires a confirmation checkbox before saving. JavaScript is optional.
JSON/CSV exports retain `catalog_number` as an alias equal to the current `id`.

Schema 4 upgrades existing records by assigning their current displayed catalog
numbers as IDs, then removing the catalog-number column. This can break old links
immediately on upgrade, particularly where displayed numbers previously differed
from record IDs. All original intake JSON, photo paths, other fields, revisions,
and timestamps are preserved.

Schema 6 separates intake allocation from SQLite's rowid high-water mark. Its
one-time migration recovers the next number from original intake IDs/numbers and
the earliest recorded move or swap for each record, falling back to the current
ID for older records with neither. This can lower a counter inflated by vanity
IDs without changing any asset, intake snapshot, or history event. Changes made
before ID history existed cannot always be reconstructed; untraceable high IDs
may still leave the initial counter higher. New intake snapshots record the
allocated ID, and later allocations advance the counter transactionally.
There are no redirects that could hide swaps by silently opening another asset.

## Description migration and early-client compatibility

Database schema 3 adds `short_description` and `details`. The bundled migration
runs once during startup for existing inventory: text before the first newline
becomes the short description, and everything after it becomes details. CRLF
first-line endings are normalized for the single-line field; body formatting,
including blank lines and indentation, is retained. The existing combined
`description`, original intake JSON, IDs, catalog numbers, timestamps, revisions,
and photo files are unchanged by migration. Single-line descriptions get empty
details. No separate migration command is required: early testers can build the
updated executable and restart it with the same data directory.

Full API responses and JSON/CSV exports include the independent fields. CSV
retains all existing columns and appends `short_description` and `details`.
Summaries continue to omit long notes. Display title retains its existing
120-character truncation; the full short-description field and field searches
preserve all text.

Schema 7 drops the redundant stored `description` column. The independent
`short_description` and `details` fields remain authoritative and are unchanged,
as are original intake JSON, IDs, revisions, timestamps, photo references, ID
history, and the intake allocator. Combined description reads and searches now
derive their value from these fields, joining nonempty details with a single LF
separator. Legacy CRLF first-line separators and an empty trailing separator
are therefore normalized in this compatibility value; body formatting is kept.
Original intake snapshots retain their original text.

The combined `description` field remains a deprecated compatibility projection.
Early clients may still create or PATCH it; the first line replaces the short
description and the rest replaces details, so those writes affect both fields.
Legacy writes retain their 20,000-byte combined limit. A request cannot mix
`description` with `short_description` or `details`; this returns HTTP 400.
Use the new fields for independent edits. Older original-intake snapshots retain
their legacy combined text; new snapshots include both fields and the projection.

## Errors

```json
{"error":{"code":"not_found","message":"Entry not found."}}
```

API error codes are `invalid_request`, `invalid_id`, `not_found`,
`method_not_allowed`, `conflict`, `unsupported_media_type`, `request_too_large`,
and `internal_error`. The authorization policy can also return `unauthorized` or
`forbidden` once a gated policy is configured. Use the HTTP status and error code
for program logic; messages are human-readable. Internal errors do not expose
database details. Original-image and thumbnail routes retain their existing
image/plain-text responses rather than the API error envelope.

The machine-readable contract is available at `/api/openapi.json` and in
[the source specification](../internal/catalog/openapi.json). It includes image
retrieval as well as inventory endpoints. No new runtime dependencies are required;
database upgrades use the bundled startup migrations described above.
