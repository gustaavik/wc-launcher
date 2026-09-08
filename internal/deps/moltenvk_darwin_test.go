package deps

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/gustaavik/wc-launcher/internal/install"
	"github.com/gustaavik/wc-launcher/internal/paths"
)

func TestEnsureInstallsTheDriverAndTheManifestBesideIt(t *testing.T) {
	layout := testLayout(t)
	serve(t, driverArchive(t, map[string][]byte{
		DylibName: []byte("not really a dylib, but not empty either"),
		"LICENSE": []byte("Apache 2.0"),
	}))

	dir, err := Ensure(context.Background(), layout, nil)
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	if want := layout.MoltenVKDir(Version); dir != want {
		t.Errorf("installed into %s, want %s", dir, want)
	}
	for _, name := range []string{DylibName, ManifestName, markerName, "LICENSE"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("%s is missing after Ensure: %v", name, err)
		}
	}

	// Nothing may be left behind: a staging directory or a partial download
	// that survived would be reused by the next attempt.
	entries, err := os.ReadDir(filepath.Dir(dir))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".") {
			t.Errorf("left %s behind", entry.Name())
		}
	}
}

// The manifest is generated rather than shipped precisely so that this holds
// wherever the driver landed. A loader that cannot resolve library_path finds
// no driver, which is indistinguishable from not installing one at all.
func TestTheGeneratedManifestPointsAtTheDylibBesideIt(t *testing.T) {
	layout := testLayout(t)
	serve(t, driverArchive(t, map[string][]byte{DylibName: []byte("driver")}))

	dir, err := Ensure(context.Background(), layout, nil)
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, ManifestName))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		ICD struct {
			LibraryPath string `json:"library_path"`
			APIVersion  string `json:"api_version"`
			Portability bool   `json:"is_portability_driver"`
		} `json:"ICD"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatalf("parse %s: %v", ManifestName, err)
	}
	resolved := filepath.Join(dir, manifest.ICD.LibraryPath)
	if _, err := os.Stat(resolved); err != nil {
		t.Errorf("library_path %q resolves to %s, which does not exist", manifest.ICD.LibraryPath, resolved)
	}
	if manifest.ICD.APIVersion != apiVersion {
		t.Errorf("api_version is %q, want %q", manifest.ICD.APIVersion, apiVersion)
	}
	// A loader hides a driver that does not admit to being non-conformant.
	if !manifest.ICD.Portability {
		t.Error("is_portability_driver is false; MoltenVK is a portability driver")
	}
}

// Called before every launch, so the common case must cost a stat and not a
// request.
func TestEnsureIsANoOpWhenTheDriverIsAlreadyInstalled(t *testing.T) {
	layout := testLayout(t)
	hits := serve(t, driverArchive(t, map[string][]byte{DylibName: []byte("driver")}))

	if _, err := Ensure(context.Background(), layout, nil); err != nil {
		t.Fatalf("first Ensure: %v", err)
	}
	if _, err := Ensure(context.Background(), layout, nil); err != nil {
		t.Fatalf("second Ensure: %v", err)
	}

	if got := hits.Load(); got != 1 {
		t.Errorf("the server was asked %d times, want 1", got)
	}
}

// A driver pinned to a different checksum is a different driver. Reinstalling
// is the only correct answer; treating the directory as good would keep
// whatever is there forever.
func TestABumpedPinReinstalls(t *testing.T) {
	layout := testLayout(t)
	hits := serve(t, driverArchive(t, map[string][]byte{DylibName: []byte("driver")}))

	if _, err := Ensure(context.Background(), layout, nil); err != nil {
		t.Fatalf("first Ensure: %v", err)
	}

	dir := layout.MoltenVKDir(Version)
	raw, err := os.ReadFile(filepath.Join(dir, markerName))
	if err != nil {
		t.Fatal(err)
	}
	var m marker
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	m.SHA256 = strings.Repeat("0", 64)
	rewritten, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, markerName), rewritten, 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := Ensure(context.Background(), layout, nil); err != nil {
		t.Fatalf("second Ensure: %v", err)
	}
	if got := hits.Load(); got != 2 {
		t.Errorf("the server was asked %d times, want 2", got)
	}
}

func TestACorruptDownloadInstallsNothing(t *testing.T) {
	layout := testLayout(t)
	serve(t, driverArchive(t, map[string][]byte{DylibName: []byte("driver")}))
	assetSHA256 = strings.Repeat("a", 64) // not what the server is serving

	if _, err := Ensure(context.Background(), layout, nil); err == nil {
		t.Fatal("Ensure accepted an archive that did not match its checksum")
	}
	if _, err := os.Stat(layout.MoltenVKDir(Version)); !os.IsNotExist(err) {
		t.Error("a driver directory was created from bytes that failed verification")
	}
	// Keeping the partial would make every later attempt resume onto bytes
	// that can never verify.
	matches, _ := filepath.Glob(filepath.Join(layout.Runtime, "moltenvk", ".download-*"))
	if len(matches) != 0 {
		t.Errorf("kept %v after a checksum failure", matches)
	}
}

// An archive of the right size and checksum that happens not to contain a
// driver still cannot be installed: the directory would look complete and fail
// at dlopen time instead, where the launcher can no longer explain it.
func TestAnArchiveWithNoDylibIsRefused(t *testing.T) {
	layout := testLayout(t)
	serve(t, driverArchive(t, map[string][]byte{"LICENSE": []byte("Apache 2.0")}))

	if _, err := Ensure(context.Background(), layout, nil); err == nil {
		t.Fatal("Ensure accepted an archive with no driver in it")
	}
	if _, err := os.Stat(layout.MoltenVKDir(Version)); !os.IsNotExist(err) {
		t.Error("an incomplete driver directory was left in place")
	}
}

// Both UpdateService.Install and GameService.Launch call Ensure, and they can
// overlap: a player pressing Play while an install runs is not exotic.
func TestConcurrentCallsInstallOnce(t *testing.T) {
	layout := testLayout(t)
	hits := serve(t, driverArchive(t, map[string][]byte{DylibName: []byte("driver")}))

	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := Ensure(context.Background(), layout, nil); err != nil {
				t.Errorf("Ensure: %v", err)
			}
		}()
	}
	wg.Wait()

	if got := hits.Load(); got != 1 {
		t.Errorf("the server was asked %d times, want 1", got)
	}
}

// The launcher refuses to install a game build with no checksum. A driver is
// code the game dlopens; it gets the same rule.
func TestAnUnpinnedDriverIsNeverInstalled(t *testing.T) {
	layout := testLayout(t)
	hits := serve(t, driverArchive(t, map[string][]byte{DylibName: []byte("driver")}))
	assetSHA256 = ""

	if _, err := Ensure(context.Background(), layout, nil); err == nil {
		t.Fatal("Ensure installed a driver with no checksum pinned")
	}
	if got := hits.Load(); got != 0 {
		t.Errorf("the server was asked %d times, want 0 — nothing should be downloaded", got)
	}
}

func TestProgressIsReportedUnderOnePhase(t *testing.T) {
	layout := testLayout(t)
	serve(t, driverArchive(t, map[string][]byte{DylibName: []byte("driver")}))

	var phases []string
	var mu sync.Mutex
	_, err := Ensure(context.Background(), layout, func(p install.Progress) {
		mu.Lock()
		defer mu.Unlock()
		phases = append(phases, p.Phase)
	})
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	if len(phases) == 0 {
		t.Fatal("no progress was reported at all")
	}
	for _, got := range phases {
		if got != phase {
			t.Errorf("reported phase %q, want every report to be %q", got, phase)
		}
	}
}

// helpers

// serve points the pinned asset at a test server and returns its hit count.
// The real pin is restored afterwards, so these tests cannot leak into each
// other or hide a bad constant.
func serve(t *testing.T, body []byte) *atomic.Int64 {
	t.Helper()

	realURL, realSHA, realSize := assetURL, assetSHA256, assetSize
	t.Cleanup(func() { assetURL, assetSHA256, assetSize = realURL, realSHA, realSize })

	var hits atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.Write(body)
	}))
	t.Cleanup(server.Close)

	sum := sha256.Sum256(body)
	assetURL = server.URL + "/moltenvk.tar.gz"
	assetSHA256 = hex.EncodeToString(sum[:])
	assetSize = int64(len(body))
	return &hits
}

// driverArchive builds what the mirror workflow publishes: one top-level
// directory, regular files only, which is all install.Extract accepts.
func driverArchive(t *testing.T, files map[string][]byte) []byte {
	t.Helper()

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	writer := tar.NewWriter(gz)

	root := "moltenvk-" + Version
	if err := writer.WriteHeader(&tar.Header{
		Name: root + "/", Typeflag: tar.TypeDir, Mode: 0o755,
	}); err != nil {
		t.Fatal(err)
	}
	for name, body := range files {
		if err := writer.WriteHeader(&tar.Header{
			Name: root + "/" + name, Typeflag: tar.TypeReg, Mode: 0o644, Size: int64(len(body)),
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// testLayout builds a layout under a temp root rather than calling paths.New,
// which would write into the developer's real application-support directory.
func testLayout(t *testing.T) paths.Layout {
	t.Helper()

	root := t.TempDir()
	layout := paths.Layout{
		Root:     root,
		Versions: filepath.Join(root, "versions"),
		Data:     filepath.Join(root, "data"),
		Logs:     filepath.Join(root, "logs"),
		Runtime:  filepath.Join(root, "runtime"),
	}
	if err := os.MkdirAll(layout.Runtime, 0o755); err != nil {
		t.Fatal(err)
	}
	return layout
}
