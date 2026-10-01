# Syscat

Quick inventory intake for collections of computer systems. Photograph a machine,
record its location and what you know, and identify or classify it later.

Phase 0 adapts Cardcat's intake workflow into an independent systems catalog. It runs as one Go executable with an embedded web UI
and SQLite database. No separate web server, database service, or frontend build
is needed.

## Build and run

With Go 1.25 or newer:

```sh
go run ./cmd/build
./syscat
```

Syscat listens on **all interfaces on port 8800** by default, ready for clients
on the server's network. Startup prints the data directory and access URLs using
the server's local interface addresses. From another computer, open
**http://<server-address>:8800**; on the server itself, use
**http://127.0.0.1:8800**. Firewall rules still determine which clients can connect.
The reported addresses are local interface addresses, not a public address
behind a router or NAT.

Startup creates `./data`. Run from the same directory next time, or choose a
fixed data directory:

```sh
./syscat --data-dir /path/to/syscat-data
```

Use `--listen` to bind a specific interface or restrict access to the server:

```sh
./syscat --listen 192.168.1.10:8800
./syscat --listen 127.0.0.1:8800
```

There is no login in phase 0; anyone who can reach the server can read and change
its inventory. Run it on your trusted workshop network.

`-p` / `--port` overrides the port in `--listen` while retaining its bind address.
Port `0` chooses an available port and prints the actual address. `--help` lists
options; Ctrl+C stops gracefully. The build helper produces `syscat.exe` on
Windows. `CGO_ENABLED=0 go run ./cmd/build` builds without a C compiler; the
resulting executable needs neither Go nor an installed SQLite.

## Intake

- One entry per physical system. An overview photo and short description are required;
  details, location, and detail photos are optional. Existing photos satisfy the overview
  requirement when editing. Older entries without photos need an overview before
  saving changes.
- Permanent record IDs remain sequential and keep all links stable. Displayed
  catalog numbers have no prefix: `00001`, `00002`, and so on. By default they
  match record IDs; if that number is occupied, new intake uses the next free
  number above it. **Change number** previews a move or a swap with the occupant.
  Archived numbers remain reserved. Display padding grows beyond five digits.
- Short description supplies the display title. Use Details for observations,
  specifications, condition, or research questions. API clients can PATCH either
  field independently. Structured classifications are deferred.
- **Save & add another** retains location in the browser session. Sessions reset
  on server restart or after 24 hours of inactivity. Cookies are required for
  saving and remembered locations in this initial Cardcat-derived baseline.
- The location picker opens only when explicitly chosen and stays out of the
  sequential keyboard tab order. Typing arbitrary locations always works.
- Search descriptions, locations, photo captions, permanent IDs, or catalog numbers, including either `42` or
  `00042`. Active and archived records have separate collection pages.
- Open an active entry to edit it, attach photographs, or mark photos for deletion
  on save. Deletion removes original files, thumbnails, and intake photo references.
  Other original intake values remain unchanged.
- Existing entries keep text fields editable; **Edit photos** reveals photo
  controls and uploads. Hiding those controls preserves pending changes, which
  **Save changes** saves. Without JavaScript, all photo controls remain available.
- Group collapse states are remembered per entry in this browser. Validation
  errors temporarily open groups so photo corrections remain accessible.
- **Undo entry** and **Archive entry** preserve the record, number, and photos;
  archived entries can be restored. Entries cannot be permanently deleted.
- Repeated intake submissions create only one entry. Revision checks prevent
  stale forms from overwriting newer edits. Validation errors retain entered
  text; photos must be reselected after an error.

## Photos

Intake requires one overview photo and offers optional detail photos. The first attached image is
used in the collection. Supporting browsers can select multiple detail files,
add more file inputs, and show local previews before saving. Without JavaScript,
ordinary uploads work; browsers without multiple selection can attach additional
photos through subsequent edits. Edits append photos without replacing originals.

There is no fixed photo-count limit per entry or save. Each save is limited to
**256 MiB total by default**, and each image to **12 MiB / 32 megapixels**.
Attach further batches through later edits. Configure the total request limit in whole MiB with:

```sh
./syscat --max-upload-mib 512
```

The form and upload errors display the configured limit. The total includes
multipart form overhead. Upload and response timeouts are 15 minutes to allow
larger batches on slower connections. Accepted formats are JPEG, PNG, and GIF;
convert HEIC to JPEG first.

New uploads remove location-capable metadata, including EXIF GPS, XMP/IPTC,
comments, and opaque vendor metadata. JPEGs retain only the EXIF orientation
needed for correct display. Image data is preserved without recompression;
display-related color metadata and GIF animation controls remain intact. Existing
uploads require a separate cleanup. JPEG thumbnails fit within 1,000 x 1,000
pixels and respect EXIF orientation. GIF thumbnails use the first frame. Failed
uploads do not save a partial entry; newly written files are removed on normal
validation or save failures. Local previews offer links to view images in a
separate tab.

## Data, exports, and backups

```text
data/
  syscat.sqlite3
  photos/
  thumbnails/
```

Keep live data on local storage on the server. Syscat starts with its own empty
inventory; importing Cardcat inventory is deferred.

Collection and Archive each offer CSV and JSON exports. Both include photo paths,
not image bytes; JSON also preserves original intake. CSV escapes potential
spreadsheet formulas. Exports alone are not complete backups.

For a consistent backup, stop Syscat and wait for it to exit, copy the **entire
data directory**, then restart with the same `--data-dir`. To restore, stop the
server, copy the backup into an empty destination, and point `--data-dir` there.
Back up before replacing the executable for an upgrade. Startup uses versioned
schema migrations and rejects unsupported newer schemas.
The schema-3 description migration runs once at startup, splitting existing text
at the first newline without rewriting original intake. Early testers can build
the updated executable and start it with their existing `--data-dir`.

## Agent and programmatic access

A JSON API provides paginated search, records with original intake, and photo URLs.
PATCH endpoints independently edit short descriptions, details, locations,
captions, and groups using
asset revision checks and stable photo IDs. Multipart POST creates new records
with original intake or attaches photos to existing records, using the shared
image privacy and thumbnail pipeline. Creation supports persistent retry keys. A dedicated catalog-number endpoint
changes or swaps display numbers without changing permanent IDs or links.
Access currently uses the trusted network without credentials or browser sessions; write routes share a policy
that can enforce authentication later.

```sh
curl http://127.0.0.1:8800/api/assets/1
curl 'http://127.0.0.1:8800/api/assets?q=00001&page_size=10'
curl 'http://127.0.0.1:8800/api/assets?view=summary&page_size=100'
curl 'http://127.0.0.1:8800/api/assets?view=summary&field=title&q=Apple'
```

See [API documentation](docs/api.md). The embedded OpenAPI description is served
at `/api/openapi.json`. The footer links to `/api`, a JSON discovery index, and
response headers advertise the OpenAPI document. The API shares the catalog's
network access model.

## Development

```sh
go test ./...
go test -race ./...
go vet ./...
```

The application version lives in `internal/buildinfo/buildinfo.go`. Footer,
startup log, and `--version` show the same `Syscat <version> <short-commit>` build
identity. The build helper adds `-modified.<unix-seconds>` for changed source;
direct `go build -o syscat ./cmd/syscat` uses `-modified`. Missing VCS metadata is
shown as `unknown`. Git is not needed at runtime. Versions, schema versions, and
record revisions are independent. Release tags use `vMAJOR.MINOR.PATCH`; see
[CHANGELOG.md](CHANGELOG.md) for release notes.

Core flows use server-rendered HTML and ordinary forms; JavaScript is optional.
The inherited browser target is approximately 2015-2016 desktop browsers and
current phones. Actual legacy browser compatibility and physical phone camera
behavior still need testing. System/component profiles, relationships,
multi-user support, and broader browser support are future work.
