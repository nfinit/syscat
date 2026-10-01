# Inventory API

The API is served by the same Syscat executable on the same port as the web UI.
It requires no browser session, cookies, or CSRF token. Like the existing catalog,
it is accessible to anyone who can reach this trusted-network server.

| Endpoint | Purpose |
| --- | --- |
| `GET /api` | API discovery index (`/api/` is also supported) |
| `GET /api/assets` | Paginated listing and search |
| `GET /api/assets/{id}` | Full current record and original intake |
| `PATCH /api/assets/{id}` | Update current description or location |
| `PATCH /api/assets/{id}/photos/{photo_id}` | Update a current photo caption or group |
| `GET /api/openapi.json` | Embedded OpenAPI 3.0.3 description |

Read endpoints support GET and HEAD; the two edit endpoints support PATCH.
Unsupported methods return HTTP 405 with the endpoint’s allowed methods. Unknown
API endpoints return HTTP 404. Responses and errors are JSON with
`Cache-Control: no-store`. API calls do not create sessions. Creation, uploads,
photo deletion/ordering, and archive operations remain web-interface features.

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
| `q` | Empty | Description/location/photo-caption substring search, or exact inventory ID |
| `field` | `all` | Restrict `q` to `all`, `title`, `description`, `location`, `caption`, or `id` |
| `page` | `1` | Page number, from 1 to 1,000,000 |
| `page_size` | `50` | Entries per page, from 1 to 100 |
| `archived` | `0` | `0`/`false` for active entries; `1`/`true` for archived entries |
| `view` | `full` | `full` for current record metadata; `summary` for compact catalog review |

Search uses the same behavior as the HTML catalog: case-insensitive text matching
under SQLite's existing LIKE rules, literal `%` and `_`, and exact numeric IDs
with or without zero padding. Caption matching searches current photos only; an
entry appears once even if multiple captions match. Results are ordered by
descending inventory ID.
`field=title` searches the complete first description line, including text beyond
the display title's truncation limit. `field=description` searches the whole
current description, including its title line. `field=location` searches only
location; `field=caption` searches current photo captions; `field=id` matches only
an exact positive numeric inventory ID (zero padding is allowed). A nonnumeric
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

- `id`, `label`, and `title` (the same display title as a full record).
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

Each record contains numeric `id`, display `label`, a short `title`, the full
`description`, `location`, ordered `photos`, `created_at`, `updated_at`,
`revision`, and `archived`. It also provides `url` for the HTML entry and
`api_url` for its JSON representation. Individual records include
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
- `original_url`: the original image, retained byte-for-byte.
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
Deleting a photo removes it from current `photos` and any original-intake photo
references. Its original and thumbnail files are deleted after a successful save.
Other original-intake fields remain unchanged.

## Editing records

Writes currently use trusted-network access without credentials. All write routes
pass through a shared authorization policy so authentication can be added later.
Browser form sessions and CSRF handling are independent of these JSON endpoints.

Read the asset first and include its current positive integer `revision` in every
PATCH. Each successful write returns the full updated asset, including its new
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
