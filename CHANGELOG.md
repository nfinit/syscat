# Changelog

## Unreleased

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
