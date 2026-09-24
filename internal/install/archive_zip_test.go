package install

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// buildZip writes a .zip of name → body; a trailing "/" makes a directory.
// The file is named as the downloader names it — a .part, whatever the asset.
func buildZip(t *testing.T, files []entry) string {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range files {
		w, err := zw.Create(e.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(e.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), ".download-wyvencraft.zip.part")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// The installer always extracts its .part file, so choosing the format by
// suffix sent every Windows .zip down the gzip path. It has to be the bytes.
func TestExtractRecognisesAZipWhateverItIsCalled(t *testing.T) {
	archive := buildZip(t, []entry{
		{name: "wyvencraft-v0.0.1-x86_64-pc-windows-msvc/"},
		{name: "wyvencraft-v0.0.1-x86_64-pc-windows-msvc/wyvencraft.exe", body: "MZ"},
		{name: "wyvencraft-v0.0.1-x86_64-pc-windows-msvc/assets/blocks.toml", body: "[block.dirt]"},
	})
	dest := t.TempDir()

	if err := Extract(archive, dest); err != nil {
		t.Fatalf("Extract: %v", err)
	}
	for _, want := range []string{"wyvencraft.exe", filepath.Join("assets", "blocks.toml")} {
		if _, err := os.Stat(filepath.Join(dest, want)); err != nil {
			t.Errorf("missing %s: %v", want, err)
		}
	}
}

func TestExtractRecognisesATarGzWhateverItIsCalled(t *testing.T) {
	tarball := buildTarGz(t, []entry{{name: "pkg/wyvencraft", body: "ELF", mode: 0o755}})
	misnamed := filepath.Join(t.TempDir(), "wyvencraft.zip.part")
	if err := os.Rename(tarball, misnamed); err != nil {
		t.Fatal(err)
	}
	dest := t.TempDir()

	if err := Extract(misnamed, dest); err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "wyvencraft")); err != nil {
		t.Errorf("missing wyvencraft: %v", err)
	}
}

func TestExtractRefusesAnUnknownFormat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "archive.part")
	if err := os.WriteFile(path, []byte("<html>Just a moment...</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Extract(path, t.TempDir()); err == nil {
		t.Fatal("want an error for bytes that are neither zip nor gzip")
	}
}

// The zip branch was unreachable until now, so its traversal guard had never
// run against a real archive.
func TestATraversingZipEntryIsRefused(t *testing.T) {
	for _, name := range []string{"pkg/../../escaped.txt", `pkg\..\..\escaped.txt`, "C:/evil.dll"} {
		archive := buildZip(t, []entry{{name: name, body: "pwned"}})
		dest := t.TempDir()
		if err := Extract(archive, dest); err == nil {
			t.Errorf("entry %q was accepted", name)
		}
		if _, err := os.Stat(filepath.Join(filepath.Dir(dest), "escaped.txt")); err == nil {
			t.Errorf("entry %q escaped", name)
		}
	}
}
