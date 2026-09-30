# Read-only inventory API

The API is served by the same Syscat executable on the same port as the web UI.
It requires no browser session, cookies, or CSRF token. Like the existing catalog,
it is accessible to anyone who can reach this trusted-network server.

| Endpoint | Purpose |
| --- | --- |
| `GET /api` | API discovery index (`/api/` is also supported) |
| `GET /api/assets` | Paginated listing and search |
| `GET /api/assets/{id}` | Full current record and original intake |
| `GET /api/openapi.json` | Embedded OpenAPI 3.0.3 description |

GET and HEAD are supported. Other methods on API endpoints return HTTP 405 with
`Allow: GET, HEAD`. Unknown API endpoints return HTTP 404. API responses and
errors are JSON with `Cache-Control: no-store`. API calls do not create sessions.
There are no API write endpoints.

## Discovery

Starting from the server address, clients can discover the API through the
`Link` response header on pages, redirects, API responses, and errors:

```http
Link: </api/openapi.json>; rel="service-desc"; type="application/json"
```

The `service-desc` relation identifies a machine-readable service description
([RFC 8631](https://www.rfc-editor.org/rfc/rfc8631)). The site footer also links
to `/api`. Both `/api` and `/api/` return a JSON index with `name`, `build`,
`read_only`, and a `links` object pointing to `self`, `assets`, and `openapi`.
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
```

| Parameter | Default | Meaning |
| --- | --- | --- |
| `q` | Empty | Description/location substring search, or exact inventory ID |
| `page` | `1` | Page number, from 1 to 1,000,000 |
| `page_size` | `50` | Entries per page, from 1 to 100 |
| `archived` | `0` | `0`/`false` for active entries; `1`/`true` for archived entries |

Search uses the same behavior as the HTML catalog: case-insensitive text matching
under SQLite's existing LIKE rules, literal `%` and `_`, and exact numeric IDs
with or without zero padding. Results are ordered by descending inventory ID.
Active and archived records are separate sets. Unknown, repeated, malformed, or
out-of-range parameters return HTTP 400. Empty `archived` is treated as false.

The response contains `assets`, `total`, `page`, `page_size`, `query`, and
`archived`. Root-relative `previous` and `next` URLs appear when another page is
available in that direction and retain the search and archive filter. An empty
result uses `"assets": []`. Lists include current descriptions and photo metadata;
original-intake snapshots are available only on individual records.

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

- `original_name`: the uploaded filename.
- `caption`: optional descriptive text, or an empty string when absent (up to 1,000 characters).
- `role`: `overview` for the first current photo, `detail` for subsequent photos.
  Choosing an overview moves it first and retains the previous overview as a detail photo.
- `original_url`: the original image, retained byte-for-byte.
- `thumbnail_url`: the orientation-corrected JPEG display image.

URLs begin with `/` and should be resolved against the server origin used for
the API request. For example, `/photos/example.jpg` from a request to
`http://10.0.0.81:8800/api/assets/1` is retrieved from
`http://10.0.0.81:8800/photos/example.jpg`. No image bytes are embedded in JSON.
Retrieve thumbnails for quick visual inspection and originals for small markings.

Original intake is returned as stored, with photo `path` and `thumbnail` values
instead of URL fields. Prefix those paths with `/` to retrieve the images from
the same server. Its snapshot ID may be `0` because it was captured before the
permanent ID was allocated; use the outer record's `id` to address the entry.

## Errors

```json
{"error":{"code":"not_found","message":"Entry not found."}}
```

API error codes are `invalid_request`, `invalid_id`, `not_found`,
`method_not_allowed`, and `internal_error`. Use the HTTP status and error code
for program logic; messages are human-readable. Internal errors do not expose
database details. Original-image and thumbnail routes retain their existing
image/plain-text responses rather than the API error envelope.

The machine-readable contract is available at `/api/openapi.json` and in
[the source specification](../internal/catalog/openapi.json). It includes image
retrieval as well as inventory endpoints. No new runtime dependencies or database
migrations are required.
