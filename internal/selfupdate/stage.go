package selfupdate

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/gustaavik/wc-launcher/internal/install"
	"github.com/gustaavik/wc-launcher/internal/paths"
)

// TeamID is the Apple Developer team the release workflow signs with.
//
// Checked against the downloaded bundle before it is ever swapped in. TLS and
// the SHA-256 already prove the bytes are the ones GitHub is serving; this
// proves GitHub is serving ours. Whoever published the release, they cannot
// have signed it without this team's Developer ID key.
const TeamID = "S6EF64ZEMD"

// checksumLimit bounds a .sha256 body. A checksum line is under 128 bytes.
const checksumLimit = 4096

// Stage downloads, verifies and unpacks a launcher build, and returns the path
// to the unpacked .app.
//
// Nothing here touches the running launcher: staging is a separate directory,
// and a failure at any point leaves the current install exactly as it was.
func Stage(ctx context.Context, layout paths.Layout, client *Client, release Release, report install.ProgressFunc) (string, error) {
	asset, err := SelectAsset(release)
	if err != nil {
		return "", err
	}

	wantHash, err := expectedHash(ctx, client, release, asset)
	if err != nil {
		return "", err
	}

	root := layout.LauncherUpdateRoot()
	if err := os.MkdirAll(root, 0o755); err != nil {
		return "", fmt.Errorf("create %s: %w", root, err)
	}

	// Kept between attempts so an interrupted download resumes.
	partial := filepath.Join(root, ".download-"+asset.Name+".part")
	if err := install.Fetch(ctx, asset.URL, partial, asset.Size, report); err != nil {
		return "", err
	}

	if err := install.Verify(partial, wantHash, report); err != nil {
		// A mismatched file will never verify, so keeping it would make every
		// later attempt resume onto corrupt bytes.
		os.Remove(partial)
		return "", err
	}

	if report != nil {
		report(install.Progress{Phase: "extracting", Percent: -1})
	}

	dir := layout.LauncherUpdateDir(release.Tag)
	if err := os.RemoveAll(dir); err != nil {
		return "", fmt.Errorf("clear %s: %w", dir, err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create %s: %w", dir, err)
	}

	if err := unpack(partial, dir); err != nil {
		return "", err
	}

	app, err := unpackedBundle(dir)
	if err != nil {
		return "", fmt.Errorf("%s: %w", asset.Name, err)
	}
	if err := verifySignature(app); err != nil {
		os.RemoveAll(dir)
		return "", err
	}

	os.Remove(partial)
	if report != nil {
		report(install.Progress{Phase: "done", Percent: 100})
	}
	return app, nil
}

// unpack expands the downloaded archive.
//
// ditto rather than archive/zip: the payload is a signed .app, and a signature
// survives only if the bundle is reproduced exactly, extended attributes and
// stapled notarization ticket included. ditto ships with macOS.
func unpack(archive, dest string) error {
	if runtime.GOOS != "darwin" {
		return fmt.Errorf("unpacking a launcher build is not supported on %s", runtime.GOOS)
	}
	cmd := exec.Command("/usr/bin/ditto", "-x", "-k", archive, dest)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("unpack the launcher build: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// unpackedBundle finds the single .app the archive contained.
func unpackedBundle(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", dir, err)
	}
	for _, entry := range entries {
		if entry.IsDir() && strings.HasSuffix(entry.Name(), ".app") {
			return filepath.Join(dir, entry.Name()), nil
		}
	}
	return "", fmt.Errorf("contains no .app bundle")
}

// verifySignature refuses a build that is not intact and not ours.
func verifySignature(app string) error {
	if runtime.GOOS != "darwin" {
		return nil
	}

	if out, err := exec.Command("/usr/bin/codesign", "--verify", "--deep", "--strict", app).CombinedOutput(); err != nil {
		return fmt.Errorf("the downloaded launcher is not correctly signed, so it was discarded: %s",
			strings.TrimSpace(string(out)))
	}

	// codesign writes the display output to stderr, hence CombinedOutput.
	out, err := exec.Command("/usr/bin/codesign", "-dv", "--verbose=4", app).CombinedOutput()
	if err != nil {
		return fmt.Errorf("could not read the downloaded launcher's signature: %s", strings.TrimSpace(string(out)))
	}
	if !strings.Contains(string(out), "TeamIdentifier="+TeamID) {
		return fmt.Errorf("the downloaded launcher is signed by an unexpected developer, so it was discarded")
	}
	return nil
}

// expectedHash finds the SHA-256 to check the download against: the digest
// GitHub published with the asset, or failing that the .sha256 sibling.
func expectedHash(ctx context.Context, client *Client, release Release, asset Asset) (string, error) {
	if hash := asset.SHA256(); hash != "" {
		return hash, nil
	}

	sibling, ok := ChecksumAsset(release, asset)
	if !ok {
		return "", fmt.Errorf("%s publishes no checksum for %s; refusing to install it unverified",
			release.Tag, asset.Name)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sibling.URL, nil)
	if err != nil {
		return "", fmt.Errorf("build the checksum request: %w", err)
	}
	resp, err := client.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrUnreachable, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, checksumLimit))
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrUnreachable, err)
	}
	hash := install.ParseChecksum(string(body))
	if hash == "" {
		return "", fmt.Errorf("could not read a checksum for %s", asset.Name)
	}
	return hash, nil
}
