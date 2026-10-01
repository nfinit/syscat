// Package buildinfo identifies the source used to build the executable.
package buildinfo

import "runtime/debug"

const Version = "1.6.1"

// modifiedAt is injected by cmd/build as Unix seconds. Plain go build leaves it
// empty because Go's native VCS metadata does not record working-file mtimes.
var modifiedAt string

var label = readLabel()

// String returns a build-time identity, independent of the runtime directory.
func String() string { return label }

func readLabel() string {
	revision := "unknown"
	modified := false
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			switch setting.Key {
			case "vcs.revision":
				if setting.Value != "" {
					revision = setting.Value
				}
			case "vcs.modified":
				modified = setting.Value == "true"
			}
		}
	}
	if len(revision) > 7 {
		revision = revision[:7]
	}
	if modified {
		revision += "-modified"
		if modifiedAt != "" {
			revision += "." + modifiedAt
		}
	}
	return "Syscat " + Version + " " + revision
}
