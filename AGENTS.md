# Versioning

- The application version is defined in `internal/buildinfo/buildinfo.go`.
  Change it for a release, not for every commit.
- During `0.x` development, increment PATCH for fixes and small refinements.
  Increment MINOR for new capabilities or breaking changes; document breaking
  changes explicitly. Reset PATCH when incrementing MINOR.
- Release `1.0.0` when the core model and compatibility/upgrade guarantees are
  settled. After that, use MAJOR for incompatible changes, MINOR for compatible
  features, and PATCH for compatible fixes. Compatibility includes documented
  CLI behavior, export formats, and supported inventory upgrades.
- Keep application versions, SQLite schema versions, and per-record revisions
  independent. Preserve existing inventory through versioned migrations; retain
  permanent entry IDs and original intake data.
- Record changes under `Unreleased` in `CHANGELOG.md`. At release time, create a
  dated version section and an annotated `vMAJOR.MINOR.PATCH` Git tag. Do not move
  or reuse release tags. The initial `v0.1.0` baseline is `dce440a`.

# Build identity

- Build from the repository root with `go run ./cmd/build` (optionally
  `CGO_ENABLED=0`). Use `-o` to select the output executable.
- The footer, startup log, and `--version` must report the same identity:
  `Syscat <version> <short-commit>`. Use Go's embedded VCS metadata for the commit
  and modified status; do not invoke Git at application runtime.
- Modified builds use `-modified.<unix-seconds>`. The helper uses the newest
  modification time among changed tracked files, including staged changes, and
  non-ignored untracked files. Deletion-only/submodule-only changes fall back to
  build time. This timestamp is an indicator, not a unique content fingerprint.
- Clean builds have no suffix. Direct `go build` remains supported and uses
  `-modified` without a timestamp. Missing VCS metadata is shown as `unknown`.
- Keep runtime data, binaries, and local `workspace/` notes ignored by Git.
  They must not affect build identity.
- Build identity describes the source at build time. Rebuild after committing
  and restart a running instance when it should display the new commit.
