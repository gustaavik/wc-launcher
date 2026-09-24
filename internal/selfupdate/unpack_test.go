package selfupdate

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func writeZip(t *testing.T, files map[string]string) string {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), ".download-launcher.zip.part")
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestUnpackExeWritesTheOneExecutable(t *testing.T) {
	archive := writeZip(t, map[string]string{"Wyvencraft.exe": "MZ"})
	dest := t.TempDir()

	if err := unpackExe(archive, dest); err != nil {
		t.Fatalf("unpackExe: %v", err)
	}
	exe, err := unpackedPayload(dest, "windows")
	if err != nil {
		t.Fatalf("unpackedPayload: %v", err)
	}
	if got, want := exe, filepath.Join(dest, "Wyvencraft.exe"); got != want {
		t.Errorf("payload = %q, want %q", got, want)
	}
	if raw, _ := os.ReadFile(exe); string(raw) != "MZ" {
		t.Errorf("payload contains %q", raw)
	}
}

// Only the file name is kept, so nothing an archive says can place the
// executable outside dest — and a name that is not a plain one is refused.
func TestUnpackExeRefusesAnythingButOnePlainExecutable(t *testing.T) {
	for name, files := range map[string]map[string]string{
		"none":         {"README.md": "hi"},
		"two":          {"a.exe": "MZ", "b.exe": "MZ"},
		"stream":       {"C:evil.exe": "MZ"},
		"not-a-plain":  {"wyv$craft.exe": "MZ"},
		"no-name-left": {"dir/.exe": "MZ"},
	} {
		t.Run(name, func(t *testing.T) {
			dest := t.TempDir()
			if err := unpackExe(writeZip(t, files), dest); err == nil {
				t.Errorf("accepted %v", files)
			}
		})
	}
}

func TestUnpackExeTakesAnExecutableFromASubdirectoryByNameOnly(t *testing.T) {
	archive := writeZip(t, map[string]string{"../../Wyvencraft.exe": "MZ"})
	dest := t.TempDir()
	if err := unpackExe(archive, dest); err != nil {
		t.Fatalf("unpackExe: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "Wyvencraft.exe")); err != nil {
		t.Errorf("not written inside dest: %v", err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(filepath.Dir(dest)), "Wyvencraft.exe")); err == nil {
		t.Error("escaped dest")
	}
}
