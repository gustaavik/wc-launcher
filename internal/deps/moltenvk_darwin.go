package deps

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/gustaavik/wc-launcher/internal/install"
	"github.com/gustaavik/wc-launcher/internal/paths"
)

// installMu serialises Ensure. Both UpdateService.Install and
// GameService.Launch call it, and two concurrent installs would race on the
// same partial download and the same rename.
var installMu sync.Mutex

// marker is what a finished install leaves behind.
type marker struct {
	Version     string `json:"version"`
	SHA256      string `json:"sha256"`
	InstalledAt string `json:"installedAt"`
}

// Ensure makes MoltenVK available and returns the directory holding the dylib
// and its ICD manifest.
//
// It is a no-op once the pinned version is installed, so calling it before
// every launch costs a stat. Progress is reported under a single phase; the
// caller forwards it to the same event the game download uses.
func Ensure(ctx context.Context, layout paths.Layout, report install.ProgressFunc) (string, error) {
	installMu.Lock()
	defer installMu.Unlock()

	dir := layout.MoltenVKDir(Version)
	if installed(dir) {
		return dir, nil
	}
	if assetSHA256 == "" {
		// Refuse rather than install unverified bytes, exactly as the game
		// installer refuses a release with no checksum.
		return "", fmt.Errorf("no checksum is pinned for MoltenVK %s", Version)
	}

	root := filepath.Dir(dir)
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", fmt.Errorf("create %s: %w", root, err)
	}

	// Kept between attempts so an interrupted download resumes.
	partial := filepath.Join(root, ".download-moltenvk-"+Version+".tar.gz.part")
	if err := install.Fetch(ctx, assetURL, partial, assetSize, relabel(report)); err != nil {
		return "", fmt.Errorf("download MoltenVK %s: %w", Version, err)
	}
	if err := install.Verify(partial, assetSHA256, relabel(report)); err != nil {
		// A mismatched file will never verify, so keeping it would make every
		// later attempt resume onto corrupt bytes.
		os.Remove(partial)
		return "", fmt.Errorf("verify MoltenVK %s: %w", Version, err)
	}

	staging := filepath.Join(root, ".staging-moltenvk-"+Version)
	if err := os.RemoveAll(staging); err != nil {
		return "", fmt.Errorf("clear %s: %w", staging, err)
	}
	if err := os.MkdirAll(staging, 0o755); err != nil {
		return "", fmt.Errorf("create %s: %w", staging, err)
	}
	defer os.RemoveAll(staging)

	if err := install.Extract(partial, staging); err != nil {
		return "", err
	}

	// An archive without the dylib is not a driver. Reject it here rather than
	// leaving a directory that looks installed and fails at dlopen time.
	dylib := filepath.Join(staging, DylibName)
	if info, err := os.Stat(dylib); err != nil || info.Size() == 0 {
		return "", fmt.Errorf("the MoltenVK %s archive contains no %s", Version, DylibName)
	}

	if err := writeManifest(filepath.Join(staging, ManifestName)); err != nil {
		return "", err
	}
	if err := writeMarker(filepath.Join(staging, markerName)); err != nil {
		return "", err
	}

	if err := os.RemoveAll(dir); err != nil {
		return "", fmt.Errorf("clear %s: %w", dir, err)
	}
	if err := os.Rename(staging, dir); err != nil {
		return "", fmt.Errorf("move the driver into %s: %w", dir, err)
	}
	os.Remove(partial)

	return dir, nil
}

// installed reports whether dir holds a complete install of the pinned version.
//
// The marker is what makes this trustworthy: the directory is renamed into
// place whole, so a marker naming the pinned checksum can only exist next to
// the bytes that produced it.
func installed(dir string) bool {
	raw, err := os.ReadFile(filepath.Join(dir, markerName))
	if err != nil {
		return false
	}
	var m marker
	if err := json.Unmarshal(raw, &m); err != nil {
		return false
	}
	if m.Version != Version || m.SHA256 != assetSHA256 {
		return false
	}
	for _, name := range []string{DylibName, ManifestName} {
		if info, err := os.Stat(filepath.Join(dir, name)); err != nil || info.Size() == 0 {
			return false
		}
	}
	return true
}

// writeManifest writes the ICD manifest a Vulkan loader reads.
//
// Generated rather than shipped: library_path is resolved relative to the
// manifest, so writing it here is what guarantees it points at the dylib
// actually installed beside it.
func writeManifest(path string) error {
	manifest := map[string]any{
		"file_format_version": "1.0.0",
		"ICD": map[string]any{
			"library_path": "./" + DylibName,
			"api_version":  apiVersion,
			// MoltenVK is not a conformant Vulkan implementation, and a loader
			// hides a driver that does not say so.
			"is_portability_driver": true,
		},
	}
	body, err := json.MarshalIndent(manifest, "", "    ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", ManifestName, err)
	}
	if err := os.WriteFile(path, append(body, '\n'), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

func writeMarker(path string) error {
	body, err := json.MarshalIndent(marker{
		Version:     Version,
		SHA256:      assetSHA256,
		InstalledAt: time.Now().UTC().Format(time.RFC3339),
	}, "", "    ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", markerName, err)
	}
	if err := os.WriteFile(path, append(body, '\n'), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// relabel rewrites the phase of everything the install helpers report.
//
// Fetch and Verify report "downloading" and "verifying", which next to a game
// download would read as the game being downloaded twice.
func relabel(report install.ProgressFunc) install.ProgressFunc {
	if report == nil {
		return nil
	}
	return func(p install.Progress) {
		p.Phase = phase
		report(p)
	}
}
