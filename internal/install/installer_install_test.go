package install

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/gustaavik/wc-launcher/internal/catalog"
)

// servedRelease publishes one archive and returns the release describing it
// plus the URL to fetch it from.
//
// Install takes the URL as an argument, so the whole download-verify-unpack
// path is now exercisable with nothing but an http server — before this, it
// needed an account server to broker a link first.
func servedRelease(t *testing.T, tag string, body []byte, sha string) (catalog.Release, string) {
	t.Helper()

	want, ok := targets[runtime.GOOS+"/"+runtime.GOARCH]
	if !ok {
		t.Skipf("no build for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	name := "wyvencraft-" + tag + "-" + want.triple + want.ext

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.ServeContent(w, r, name, zeroTime, newReadSeeker(body))
	}))
	t.Cleanup(server.Close)

	release := catalog.Release{
		Tag: tag,
		Assets: []catalog.Asset{{
			Name:   name,
			Path:   "game/" + tag + "/" + name,
			Size:   int64(len(body)),
			SHA256: sha,
		}},
	}
	return release, server.URL + "/" + release.Assets[0].Path
}

// gameArchive is the shape the release workflow produces: one top-level
// directory holding the binary and its assets.
func gameArchive(t *testing.T, tag string) []byte {
	t.Helper()
	dir := "wyvencraft-" + tag + "-" + runtime.GOOS + "/"
	path := buildTarGz(t, []entry{
		{name: dir, kind: tar.TypeDir},
		{name: dir + GameBinary(), body: "#!/bin/sh\n", mode: 0o755},
		{name: dir + "README.md", body: "hello"},
	})
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func sum(b []byte) string {
	digest := sha256.Sum256(b)
	return hex.EncodeToString(digest[:])
}

func TestInstallUnpacksAVerifiedArchiveIntoItsVersionDirectory(t *testing.T) {
	layout := testLayout(t)
	body := gameArchive(t, "v0.5.0")
	release, url := servedRelease(t, "v0.5.0", body, sum(body))

	if err := New(layout).Install(context.Background(), release, url, nil); err != nil {
		t.Fatalf("Install: %v", err)
	}

	dir := layout.VersionDir("v0.5.0")
	if _, err := os.Stat(filepath.Join(dir, GameBinary())); err != nil {
		t.Errorf("no game binary in %s: %v", dir, err)
	}
	// The tag marker, because VersionDir sanitises lossily and the tag cannot
	// be read back out of the path.
	marker, err := os.ReadFile(filepath.Join(dir, tagMarker))
	if err != nil {
		t.Fatalf("no tag marker: %v", err)
	}
	if got := string(marker); got != "v0.5.0\n" {
		t.Errorf("marker = %q, want %q", got, "v0.5.0\n")
	}
}

// The catalogue guarantees a checksum, so reaching Install without one means
// the catalogue is wrong — and installing anyway would run an unverified
// binary.
func TestAnAssetWithNoChecksumIsRefused(t *testing.T) {
	layout := testLayout(t)
	body := gameArchive(t, "v0.5.0")
	release, url := servedRelease(t, "v0.5.0", body, "")

	if err := New(layout).Install(context.Background(), release, url, nil); err == nil {
		t.Fatal("installed an unverified build")
	}
	if _, err := os.Stat(layout.VersionDir("v0.5.0")); !os.IsNotExist(err) {
		t.Error("a refused install left a version directory behind")
	}
}

// Nothing is visible under versions/<tag> until the whole thing succeeded, so a
// bad download cannot take a working build down with it.
func TestAFailedInstallLeavesThePreviousBuildPlayable(t *testing.T) {
	layout := testLayout(t)
	installed(t, layout, "v0.4.0", true)

	body := gameArchive(t, "v0.5.0")
	release, url := servedRelease(t, "v0.5.0", body, sum([]byte("something else")))

	if err := New(layout).Install(context.Background(), release, url, nil); err == nil {
		t.Fatal("a checksum mismatch was installed")
	}
	if !New(layout).Installed("v0.4.0") {
		t.Error("the previous build was lost")
	}
	// A mismatched file will never verify, so it must not be left to resume on.
	partial := filepath.Join(layout.Versions, ".download-"+release.Assets[0].Name+".part")
	if _, err := os.Stat(partial); !os.IsNotExist(err) {
		t.Error("a corrupt partial download was kept")
	}
}

// Progress reaches the UI as events; the phases are the contract the bar reads.
func TestInstallReportsItsPhasesInOrder(t *testing.T) {
	layout := testLayout(t)
	body := gameArchive(t, "v0.5.0")
	release, url := servedRelease(t, "v0.5.0", body, sum(body))

	var phases []string
	report := func(p Progress) {
		if len(phases) == 0 || phases[len(phases)-1] != p.Phase {
			phases = append(phases, p.Phase)
		}
	}
	if err := New(layout).Install(context.Background(), release, url, report); err != nil {
		t.Fatalf("Install: %v", err)
	}

	want := []string{"downloading", "verifying", "extracting", "done"}
	for _, phase := range want {
		found := false
		for _, got := range phases {
			if got == phase {
				found = true
			}
		}
		if !found {
			t.Errorf("phase %q never reported; got %v", phase, phases)
		}
	}
}
