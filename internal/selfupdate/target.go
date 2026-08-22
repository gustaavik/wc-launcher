package selfupdate

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Target is the thing on disk an update has to replace.
type Target struct {
	// Path is the .app bundle on macOS, or the executable itself elsewhere.
	Path string
	// Bundle is true when Path is a macOS .app rather than a bare binary.
	Bundle bool
	// Writable is false when the install lives somewhere this process cannot
	// replace it — /Applications owned by another user, a read-only volume.
	// Worth knowing before a download rather than after one.
	Writable bool
}

// Locate resolves what this running launcher would have to replace to update.
//
// On macOS the executable sits at <App>.app/Contents/MacOS/<name>, and it is
// the bundle that must be swapped, not the binary inside it: replacing only the
// Mach-O would break the code signature that Gatekeeper checks on every launch.
func Locate() (Target, error) {
	exe, err := os.Executable()
	if err != nil {
		return Target{}, fmt.Errorf("locate this launcher: %w", err)
	}
	// A symlink on the path would otherwise make the update replace the link.
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}

	target := Target{Path: exe}
	if app, ok := bundleOf(exe); ok {
		target.Path = app
		target.Bundle = true
	}
	target.Writable = writable(filepath.Dir(target.Path))
	return target, nil
}

// bundleOf walks up from <App>.app/Contents/MacOS/<name> to <App>.app.
func bundleOf(exe string) (string, bool) {
	macos := filepath.Dir(exe)
	if filepath.Base(macos) != "MacOS" {
		return "", false
	}
	contents := filepath.Dir(macos)
	if filepath.Base(contents) != "Contents" {
		return "", false
	}
	app := filepath.Dir(contents)
	if !strings.HasSuffix(app, ".app") {
		return "", false
	}
	return app, true
}

// writable reports whether a directory can be written to, by trying it. The
// permission bits alone do not answer the question: a read-only mount, an
// immutable flag and an ACL all deny a write that the mode says is allowed.
func writable(dir string) bool {
	probe, err := os.CreateTemp(dir, ".wcl-update-probe-*")
	if err != nil {
		return false
	}
	name := probe.Name()
	probe.Close()
	os.Remove(name)
	return true
}

// bundleExecutable is the binary inside a .app, without hardcoding its name.
func bundleExecutable(app string) (string, error) {
	dir := filepath.Join(app, "Contents", "MacOS")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", dir, err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			return filepath.Join(dir, entry.Name()), nil
		}
	}
	return "", fmt.Errorf("%s contains no executable", dir)
}
