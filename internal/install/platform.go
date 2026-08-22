// Package install downloads, verifies and unpacks game builds.
package install

import (
	"fmt"
	"runtime"
	"strings"

	"github.com/gustaavik/wc-launcher/internal/wcauth"
)

// target is the Rust target triple this launcher's platform needs, plus the
// archive extension the release workflow packs it with.
type target struct {
	triple string
	ext    string
}

// targets maps GOOS/GOARCH onto what the game's release workflow publishes.
//
// Only the macOS and Linux rows exist upstream today; the Windows row is here
// so that adding a CI target is the only work required, rather than a launcher
// change as well.
var targets = map[string]target{
	"darwin/arm64":  {"aarch64-apple-darwin", ".tar.gz"},
	"darwin/amd64":  {"x86_64-apple-darwin", ".tar.gz"},
	"linux/amd64":   {"x86_64-unknown-linux-gnu", ".tar.gz"},
	"linux/arm64":   {"aarch64-unknown-linux-gnu", ".tar.gz"},
	"windows/amd64": {"x86_64-pc-windows-msvc", ".zip"},
}

// ErrNoBuild reports that a release exists but carries nothing for this
// platform. Worth distinguishing from a download failure: retrying will not
// help, and the player needs to be told the build simply is not published.
type ErrNoBuild struct {
	Tag      string
	Platform string
}

func (e *ErrNoBuild) Error() string {
	return fmt.Sprintf("%s has no build for %s", e.Tag, e.Platform)
}

// SelectAsset picks the archive to install from a release's assets.
//
// Matching is on the target triple rather than on exact file names, so a change
// to the release workflow's naming does not silently stop finding builds. The
// `.sha256` siblings are excluded by requiring the archive extension last.
func SelectAsset(release wcauth.Release) (wcauth.Asset, error) {
	platform := runtime.GOOS + "/" + runtime.GOARCH
	want, ok := targets[platform]
	if !ok {
		return wcauth.Asset{}, &ErrNoBuild{Tag: release.Tag, Platform: platform}
	}

	for _, asset := range release.Assets {
		if strings.Contains(asset.Name, want.triple) && strings.HasSuffix(asset.Name, want.ext) {
			return asset, nil
		}
	}
	return wcauth.Asset{}, &ErrNoBuild{Tag: release.Tag, Platform: platform}
}

// ChecksumAsset finds the `.sha256` sibling of an archive, for releases whose
// assets predate GitHub publishing a digest.
func ChecksumAsset(release wcauth.Release, archive wcauth.Asset) (wcauth.Asset, bool) {
	for _, asset := range release.Assets {
		if asset.Name == archive.Name+".sha256" {
			return asset, true
		}
	}
	return wcauth.Asset{}, false
}

// GameBinary is the executable's name inside an unpacked build.
func GameBinary() string {
	if runtime.GOOS == "windows" {
		return "wyvencraft.exe"
	}
	return "wyvencraft"
}
