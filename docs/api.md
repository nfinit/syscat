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
| `PATCH /api/assets/{id}` | Update current description or location |
| `PATCH /api/assets/{id}/photos/{photo_id}` | Update a current photo caption or group |
| `POST /api/assets/{id}/photos` | Attach one or more photos to an existing asset |
| `POST /api/assets/{id}/catalog-number` | Move or swap a catalog number |
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
| `q` | Empty | Description/location/photo-caption substring search, or exact permanent ID/catalog number |
| `field` | `all` | Restrict `q` to `all`, `title`, `description`, `location`, `caption`, `id`, or `catalog_number` |
| `page` | `1` | Page number, from 1 to 1,000,000 |
| `page_size` | `50` | Entries per page, from 1 to 100 |
| `archived` | `0` | `0`/`false` for active entries; `1`/`true` for archived entries |
| `view` | `full` | `full` for current record metadata; `summary` for compact catalog review |

Search uses the same behavior as the HTML catalog: case-insensitive text matching
under SQLite's existing LIKE rules, literal `%` and `_`, and exact numeric IDs
with or without zero padding. Caption matching searches current photos only; an
entry appears once even if multiple captions match. Results are ordered by
descending current catalog number.
`field=title` searches the complete first description line, including text beyond
the display title's truncation limit. `field=description` searches the whole
current description, including its title line. `field=location` searches only
location; `field=caption` searches current photo captions; `field=id` matches only
an exact positive permanent record ID (zero padding is allowed).
`field=catalog_number` matches only the current catalog number. Broad search
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

- Permanent `id`, mutable `catalog_number`, display `label`, and `title`.
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

Each record contains permanent numeric `id`, mutable `catalog_number`, display
`label`, a short `title`, the full
`description`, `location`, ordered `photos`, `created_at`, `updated_at`,
`revision`, and `archived`. It also provides `url` for the HTML entry and
`api_url` for its JSON representation, `photo_upload_url` for attaching photos,
and `catalog_number_url` for renumbering. URLs always use permanent `id`, never
`catalog_number`.
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
permanent ID was allocated; use the outer record's `id` to address the entry.
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

For asset text, supply `description`, `location`, or both:

```sh
curl -X PATCH http://127.0.0.1:8800/api/assets/1 \
  -H 'Content-Type: application/json' \
  -d '{"revision":7,"description":"Researched identification and notes."}'
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

Omitted fields remain unchanged. Text is trimmed; an empty string clears location,
caption, or group. Description must remain nonempty. Setting a detail photo’s group
normalizes section ordering while preserving existing group order where possible;
the overview cannot join a named group. IDs, paths, image bytes, intake, timestamps,
and archive state cannot be supplied as editable fields.

Bodies must be a single JSON object with `Content-Type: application/json`, limited
to 128 KiB, with revision and at least one editable field. Null values, unknown or
duplicate fields, invalid types, and query parameters are rejected. Limits match
the web form: description 20,000 UTF-8 bytes, location 500 UTF-8 bytes, caption
1,000 characters, and single-line group names 100 characters.

## Creating records

POST multipart form data to `/api/assets`, with a nonempty `description` and
exactly one `overview` file. `location`, `caption_overview`, and any number of
`photos` detail files are optional. Description is trimmed and limited to 20,000
UTF-8 bytes, with its first line supplying the title; location is trimmed and
limited to 500 UTF-8 bytes. Both scalar fields must appear at most once.
The overview is always ungrouped. Detail `caption_photos` and `group_photos`
follow the same file-order/count rules as attachment uploads below. Captions
are limited to 1,000 characters and single-line groups to 100 characters.

```sh
curl http://127.0.0.1:8800/api/assets \
  -H 'Idempotency-Key: recycler-intake-2026-09-30-example' \
  --form-string 'description=Unidentified desktop system' \
  --form-string 'location=Workbench' \
  -F 'overview=@system.jpg' \
  --form-string 'caption_overview=As acquired' \
  -F 'photos=@motherboard.jpg' \
  --form-string 'caption_photos=Motherboard markings' \
  --form-string 'group_photos=Interior'
```

Creation uses the same image formats, metadata stripping, thumbnails, and upload
limits as attachments. HTTP 201 returns the full asset with its permanent ID,
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

## Catalog numbers and permanent IDs

`id` is the permanent sequential record identity. `catalog_number` is the mutable,
unique number shown in `label` and the browser. Existing inventory upgrades assign
`catalog_number=id` without changing intake or revisions. New records prefer the
same number as their sequential ID; if a future number has already been assigned,
they use the first available number above that ID. Archived numbers remain reserved.

To move a record to an unused number, POST JSON to its `catalog_number_url`:

```sh
curl -X POST http://127.0.0.1:8800/api/assets/43/catalog-number \
  -H 'Content-Type: application/json' \
  -d '{"revision":6,"catalog_number":100}'
```

To use an occupied number, first find its occupant with
`GET /api/assets?field=catalog_number&q=1` (check `archived=1` too if needed), then
read both full records. Supply that occupant's permanent ID and current revision:

```sh
curl -X POST http://127.0.0.1:8800/api/assets/43/catalog-number \
  -H 'Content-Type: application/json' \
  -d '{"revision":6,"catalog_number":1,"swap_id":1,"swap_revision":1}'
```

Replace all example revisions with current values. The source takes the requested
number; its occupant takes the source's old number. HTTP 200 returns `asset` and,
for a swap, `swapped_asset`, each a full record. Both revisions advance on a swap;
only the source advances on a move. Requesting the source's current number with
no swap fields is a no-op. Permanent IDs, all URLs, image IDs/files, descriptions,
archive status, and original intake remain with their records.

Occupied numbers require **both** `swap_id` and `swap_revision`; omit both for an
unused number. Stale source/target revisions, wrong occupant IDs, or a changed
assignment return HTTP 409 and leave both records unchanged. Archived sources
must be restored before renumbering; archived occupants may participate in an
explicit swap. JSON parsing, limits, and authorization match PATCH. Normal PATCH
and creation do not accept editable catalog numbers; use this dedicated endpoint.

The browser's **Change number** control previews the affected systems before
applying the move or swap, with the same revision checks and no JavaScript required.
Collection and API lists now sort by descending catalog number. JSON exports add
`catalog_number`; CSV preserves its existing columns and appends `catalog_number`,
with `id` remaining permanent and `label` reflecting the current catalog number.

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
retrieval as well as inventory endpoints. No new runtime dependencies or database
migrations are required.
