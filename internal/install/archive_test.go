package install

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

type entry struct {
	name string
	body string
	mode int64
	kind byte
	link string
}

func buildTarGz(t *testing.T, entries []entry) string {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	for _, e := range entries {
		kind := e.kind
		if kind == 0 {
			kind = tar.TypeReg
		}
		mode := e.mode
		if mode == 0 {
			mode = 0o644
		}
		header := &tar.Header{
			Name:     e.name,
			Mode:     mode,
			Size:     int64(len(e.body)),
			Typeflag: kind,
			Linkname: e.link,
		}
		if kind != tar.TypeReg {
			header.Size = 0
		}
		if err := tw.WriteHeader(header); err != nil {
			t.Fatal(err)
		}
		if kind == tar.TypeReg {
			if _, err := tw.Write([]byte(e.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), "archive.tar.gz")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// The shape the game's release workflow actually produces: one top-level
// directory holding the binary, README and assets/.
func TestExtractStripsTheSingleTopLevelDirectory(t *testing.T) {
	archive := buildTarGz(t, []entry{
		{name: "wyvencraft-v0.0.1-aarch64-apple-darwin/", kind: tar.TypeDir},
		{name: "wyvencraft-v0.0.1-aarch64-apple-darwin/wyvencraft", body: "ELF", mode: 0o755},
		{name: "wyvencraft-v0.0.1-aarch64-apple-darwin/README.md", body: "hi"},
		{name: "wyvencraft-v0.0.1-aarch64-apple-darwin/assets/", kind: tar.TypeDir},
		{name: "wyvencraft-v0.0.1-aarch64-apple-darwin/assets/blocks.toml", body: "[block.dirt]"},
	})
	dest := t.TempDir()

	if err := Extract(archive, dest); err != nil {
		t.Fatalf("Extract: %v", err)
	}

	// The binary must sit directly in dest, because dest becomes the game's
	// working directory and assets/ is resolved against it.
	for _, want := range []string{"wyvencraft", "README.md", filepath.Join("assets", "blocks.toml")} {
		if _, err := os.Stat(filepath.Join(dest, want)); err != nil {
			t.Errorf("missing %s: %v", want, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dest, "wyvencraft-v0.0.1-aarch64-apple-darwin")); err == nil {
		t.Error("the top-level directory should have been stripped")
	}
}

// tar preserves the mode, and the game will not start if it is lost.
func TestTheGameBinaryStaysExecutable(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no executable bit on Windows")
	}
	archive := buildTarGz(t, []entry{
		{name: "pkg/wyvencraft", body: "ELF", mode: 0o755},
	})
	dest := t.TempDir()

	if err := Extract(archive, dest); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(filepath.Join(dest, "wyvencraft"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Errorf("mode = %o, want the executable bit set", info.Mode().Perm())
	}
}

// The archive is downloaded. A traversing entry must not be able to write
// outside the version directory.
func TestATraversingEntryIsRefused(t *testing.T) {
	for _, name := range []string{
		"pkg/../../escaped.txt",
		"../escaped.txt",
		"/etc/passwd",
		`pkg\..\..\escaped.txt`,
		"C:/Windows/System32/evil.dll",
	} {
		archive := buildTarGz(t, []entry{{name: name, body: "pwned"}})
		dest := t.TempDir()

		if err := Extract(archive, dest); err == nil {
			t.Errorf("entry %q was accepted", name)
		}

		// And nothing landed next to dest either.
		if _, err := os.Stat(filepath.Join(filepath.Dir(dest), "escaped.txt")); err == nil {
			t.Errorf("entry %q escaped to %s", name, filepath.Dir(dest))
		}
	}
}

// A symlink is the other way out of dest, and a real build has none.
func TestASymlinkEntryIsRefused(t *testing.T) {
	archive := buildTarGz(t, []entry{
		{name: "pkg/wyvencraft", body: "ELF", mode: 0o755},
		{name: "pkg/sneaky", kind: tar.TypeSymlink, link: "/etc/passwd"},
	})

	if err := Extract(archive, t.TempDir()); err == nil {
		t.Fatal("a symlink entry should be refused")
	}
}

func TestACorruptArchiveFailsRatherThanProducingAPartialInstall(t *testing.T) {
	path := filepath.Join(t.TempDir(), "archive.tar.gz")
	if err := os.WriteFile(path, []byte("this is not gzip"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := Extract(path, t.TempDir()); err == nil {
		t.Fatal("want an error for a corrupt archive")
	}
}

func TestResolveStripsOnlyTheFirstSegment(t *testing.T) {
	dest := "/dest"
	got, ok := resolve(dest, "pkg/assets/textures/blocks/dirt.png")
	if !ok {
		t.Fatal("should be allowed")
	}
	if want := filepath.Join(dest, "assets", "textures", "blocks", "dirt.png"); got != want {
		t.Errorf("resolve = %q, want %q", got, want)
	}
}

func TestResolveTreatsTheTopLevelDirectoryItselfAsNothingToCreate(t *testing.T) {
	got, ok := resolve("/dest", "wyvencraft-v0.0.1-aarch64-apple-darwin/")
	if !ok {
		t.Fatal("the top-level directory should be allowed")
	}
	if got != "" {
		t.Errorf("resolve = %q, want it skipped", got)
	}
}
