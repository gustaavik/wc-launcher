package selfupdate

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBundleOfWalksUpToTheApp(t *testing.T) {
	// Replacing only the Mach-O would leave a bundle whose signature no longer
	// matches its contents, which Gatekeeper refuses to launch. The whole .app
	// is the unit that gets swapped.
	app := filepath.Join("/Applications", "Wyvencraft.app")
	exe := filepath.Join(app, "Contents", "MacOS", "Wyvencraft")

	got, ok := bundleOf(exe)
	if !ok {
		t.Fatalf("bundleOf(%q) found no bundle", exe)
	}
	if got != app {
		t.Errorf("bundleOf = %q, want %q", got, app)
	}
}

func TestBundleOfIgnoresAnythingThatIsNotABundle(t *testing.T) {
	for _, exe := range []string{
		"/usr/local/bin/wc-launcher",
		"/Applications/Wyvencraft.app/Contents/Wyvencraft",
		"/somewhere/MacOS/Wyvencraft",
		"/somewhere/NotAnApp/Contents/MacOS/Wyvencraft",
	} {
		if app, ok := bundleOf(exe); ok {
			t.Errorf("bundleOf(%q) = %q, want no bundle", exe, app)
		}
	}
}

func TestWritableProbesRatherThanTrustingTheMode(t *testing.T) {
	dir := t.TempDir()
	if !writable(dir) {
		t.Errorf("a fresh temp directory reported unwritable")
	}

	// A mode alone is not the answer, but it is the one case a test can set up
	// portably, and it must be respected.
	locked := filepath.Join(dir, "locked")
	if err := os.Mkdir(locked, 0o500); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if os.Geteuid() == 0 {
		t.Skip("root writes anywhere, so there is nothing to observe")
	}
	if writable(locked) {
		t.Errorf("a read-only directory reported writable")
	}

	if writable(filepath.Join(dir, "missing")) {
		t.Errorf("a directory that does not exist reported writable")
	}
}

func TestLocateFindsSomethingReplaceable(t *testing.T) {
	target, err := Locate()
	if err != nil {
		t.Fatalf("Locate: %v", err)
	}
	if target.Path == "" {
		t.Fatal("Locate returned no path")
	}
	if !filepath.IsAbs(target.Path) {
		t.Errorf("Path = %q, want an absolute path", target.Path)
	}
	// The test binary is a bare executable, not a bundle.
	if target.Bundle {
		t.Errorf("Path = %q was taken for a bundle", target.Path)
	}
}

func TestBundleExecutableDoesNotHardcodeTheName(t *testing.T) {
	app := filepath.Join(t.TempDir(), "Renamed.app")
	macos := filepath.Join(app, "Contents", "MacOS")
	if err := os.MkdirAll(macos, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	want := filepath.Join(macos, "Something Else")
	if err := os.WriteFile(want, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write: %v", err)
	}

	got, err := bundleExecutable(app)
	if err != nil {
		t.Fatalf("bundleExecutable: %v", err)
	}
	if got != want {
		t.Errorf("bundleExecutable = %q, want %q", got, want)
	}
}
