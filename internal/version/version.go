// Package version reports which build of the launcher this is.
//
// The value is stamped in by the release workflow, which overwrites the
// embedded VERSION file with the release tag before building. A checkout that
// nobody stamped says "dev", and a dev build never offers to update itself:
// there is no released tag it could sensibly compare against.
//
// Embedding a file rather than using -ldflags -X is deliberate. The wails
// Taskfiles under build/ hardcode their own -ldflags and offer no hook to
// append to them, so linker injection would mean editing three generated files
// that `wails3 update build-assets` may later regenerate.
package version

import (
	_ "embed"
	"strings"
)

//go:embed VERSION
var raw string

// Current is the release tag this build was cut from, e.g. "v0.1.0", or "dev".
var Current = strings.TrimSpace(raw)

// IsDev reports whether this build came from a checkout rather than a release.
func IsDev() bool { return Current == "" || Current == "dev" }
