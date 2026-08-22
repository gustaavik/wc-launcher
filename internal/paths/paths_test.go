package paths

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestAppDataRootMatchesThePlatformConvention(t *testing.T) {
	root, err := appDataRoot()
	if err != nil {
		t.Fatalf("appDataRoot: %v", err)
	}
	if !filepath.IsAbs(root) {
		t.Fatalf("app-data root should be absolute, got %q", root)
	}

	// These are the exact directories the game's dirs::data_dir resolves to.
	// If one side moves, players lose their saves, so the expectation is
	// spelled out rather than derived.
	var want string
	switch runtime.GOOS {
	case "darwin":
		want = filepath.Join("Library", "Application Support")
	case "windows":
		want = "AppData"
	default:
		want = filepath.Join(".local", "share")
	}
	if !strings.Contains(root, want) {
		t.Errorf("app-data root %q does not contain %q", root, want)
	}
}

func TestLayoutKeepsDataOutsideVersions(t *testing.T) {
	// The whole point of the split: applying an update deletes and recreates a
	// version directory, so a save inside one would not survive.
	l := Layout{
		Root:     "/root",
		Versions: "/root/versions",
		Data:     "/root/data",
		Logs:     "/root/logs",
	}

	for _, path := range []string{l.Data, l.ProfileFile(), l.KeysFile()} {
		if strings.HasPrefix(path, l.Versions) {
			t.Errorf("%q lives under versions/ and would be lost on update", path)
		}
	}
}

func TestVersionDirContainsATagsFiles(t *testing.T) {
	l := Layout{Versions: "/root/versions"}

	if got, want := l.VersionDir("v0.0.1"), filepath.Join("/root/versions", "v0.0.1"); got != want {
		t.Errorf("VersionDir = %q, want %q", got, want)
	}
}

// A tag is a string from a remote server. Without sanitising it, VersionDir
// would happily point outside the app-data directory, and installing a release
// would become an arbitrary-write.
func TestAHostileTagCannotEscapeTheVersionsDirectory(t *testing.T) {
	l := Layout{Versions: "/root/versions"}

	for _, tag := range []string{
		"../../etc",
		"..",
		".",
		"/absolute",
		`..\..\windows`,
		"",
		"v1/../../..",
	} {
		dir := l.VersionDir(tag)
		if filepath.Dir(dir) != l.Versions {
			t.Errorf("tag %q produced %q, which is not directly under %q", tag, dir, l.Versions)
		}
		base := filepath.Base(dir)
		if base == "." || base == ".." || strings.HasPrefix(base, ".") {
			t.Errorf("tag %q produced %q, which is a traversal or hidden name", tag, base)
		}
	}
}

func TestOrdinaryTagsSurviveSanitisingUnchanged(t *testing.T) {
	for _, tag := range []string{"v0.0.1", "v1.2.3-rc.1", "2026.08.22", "v1_0"} {
		if got := safeTag(tag); got != tag {
			t.Errorf("safeTag(%q) = %q, want it unchanged", tag, got)
		}
	}
}

func TestLauncherUpdateStagingIsOutsideVersions(t *testing.T) {
	// The game installer prunes versions/ down to two builds. A staged launcher
	// living there would be deleted to make room for a game update.
	l := Layout{
		Root:     "/root",
		Versions: "/root/versions",
	}

	for _, path := range []string{l.LauncherUpdateRoot(), l.LauncherUpdateDir("v0.2.0")} {
		if strings.HasPrefix(path, l.Versions) {
			t.Errorf("%q lives under versions/ and would be pruned", path)
		}
		if !strings.HasPrefix(path, l.Root) {
			t.Errorf("%q lives outside the launcher's own directory", path)
		}
	}
}

// The same argument as TestAHostileTagCannotEscapeTheVersionsDirectory: the tag
// comes from GitHub, and LauncherUpdateDir is a path the launcher writes to.
func TestAHostileTagCannotEscapeTheLauncherUpdateDirectory(t *testing.T) {
	l := Layout{Root: "/root"}

	for _, tag := range []string{"../../etc", "..", ".", "/absolute", `..\..\windows`, "", "v1/../../.."} {
		dir := l.LauncherUpdateDir(tag)
		if filepath.Dir(dir) != l.LauncherUpdateRoot() {
			t.Errorf("tag %q produced %q, which is not directly under %q", tag, dir, l.LauncherUpdateRoot())
		}
		if base := filepath.Base(dir); base == "." || base == ".." || strings.HasPrefix(base, ".") {
			t.Errorf("tag %q produced %q, which is a traversal or hidden name", tag, base)
		}
	}
}
