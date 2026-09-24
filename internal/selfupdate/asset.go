package selfupdate

import (
	"runtime"
	"strings"

	"github.com/gustaavik/wc-launcher/internal/install"
)

// targets maps GOOS/GOARCH onto the asset the launcher's release workflow
// publishes for it.
//
// Both Mac rows point at the same file: the workflow lipos arm64 and amd64 into
// one universal bundle, which is also what makes a single notarization cover
// every Mac. Windows takes the .zip holding the bare executable, never the
// -setup.exe beside it: the installer is for a first install from the website,
// and an update only has to swap the one file. Linux has no row because no job
// builds it yet; adding one is a CI change plus a row here.
var targets = map[string]string{
	"darwin/arm64":  "-macos-universal.zip",
	"darwin/amd64":  "-macos-universal.zip",
	"windows/amd64": "-windows-amd64.zip",
}

// SelectAsset picks the launcher build to install from a release's assets.
//
// Matching on the suffix excludes the .sha256 siblings for free.
func SelectAsset(release Release) (Asset, error) {
	return selectAssetFor(runtime.GOOS+"/"+runtime.GOARCH, release)
}

func selectAssetFor(platform string, release Release) (Asset, error) {
	suffix, ok := targets[platform]
	if !ok {
		return Asset{}, &install.ErrNoBuild{Tag: release.Tag, Platform: platform}
	}

	for _, asset := range release.Assets {
		if strings.HasSuffix(asset.Name, suffix) {
			return asset, nil
		}
	}
	return Asset{}, &install.ErrNoBuild{Tag: release.Tag, Platform: platform}
}

// ChecksumAsset finds the .sha256 sibling of an archive, for releases whose
// assets predate GitHub publishing a digest.
func ChecksumAsset(release Release, archive Asset) (Asset, bool) {
	for _, asset := range release.Assets {
		if asset.Name == archive.Name+".sha256" {
			return asset, true
		}
	}
	return Asset{}, false
}
