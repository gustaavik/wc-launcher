package selfupdate

import (
	"errors"
	"runtime"
	"testing"

	"github.com/gustaavik/wc-launcher/internal/install"
)

func macRelease() Release {
	return Release{
		Tag: "v0.2.0",
		Assets: []Asset{
			{Name: "wc-launcher-v0.2.0-macos-universal.zip.sha256"},
			{Name: "wc-launcher-v0.2.0-macos-universal.zip"},
		},
	}
}

func TestSelectAssetPrefersTheArchiveOverItsChecksum(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skipf("no launcher build is published for %s", runtime.GOOS)
	}

	asset, err := SelectAsset(macRelease())
	if err != nil {
		t.Fatalf("SelectAsset: %v", err)
	}
	// The .sha256 sibling is listed first and also contains the platform
	// suffix. Matching on the archive extension last is what excludes it.
	if got, want := asset.Name, "wc-launcher-v0.2.0-macos-universal.zip"; got != want {
		t.Errorf("SelectAsset = %q, want %q", got, want)
	}
}

func TestBothMacArchitecturesTakeTheUniversalBuild(t *testing.T) {
	// One bundle for both, which is what makes a single notarization enough.
	if targets["darwin/arm64"] != targets["darwin/amd64"] {
		t.Errorf("the two Mac rows disagree: %q and %q", targets["darwin/arm64"], targets["darwin/amd64"])
	}
}

func TestAReleaseWithNothingForThisPlatformIsNotAnUpdate(t *testing.T) {
	release := Release{Tag: "v0.2.0", Assets: []Asset{
		{Name: "wc-launcher-v0.2.0-windows-amd64.zip"},
	}}

	_, err := SelectAsset(release)
	if err == nil {
		t.Fatal("SelectAsset accepted a release with no build for this platform")
	}
	// The UI already knows how to phrase this one, and it must not look like a
	// download failure: retrying will never help.
	var noBuild *install.ErrNoBuild
	if !errors.As(err, &noBuild) {
		t.Fatalf("got %T, want *install.ErrNoBuild", err)
	}
	if noBuild.Tag != "v0.2.0" {
		t.Errorf("Tag = %q, want v0.2.0", noBuild.Tag)
	}
}

func TestChecksumAssetFindsTheSibling(t *testing.T) {
	release := macRelease()
	archive := Asset{Name: "wc-launcher-v0.2.0-macos-universal.zip"}

	sibling, ok := ChecksumAsset(release, archive)
	if !ok {
		t.Fatal("the .sha256 sibling was not found")
	}
	if got, want := sibling.Name, archive.Name+".sha256"; got != want {
		t.Errorf("ChecksumAsset = %q, want %q", got, want)
	}

	if _, ok := ChecksumAsset(Release{}, archive); ok {
		t.Error("a release with no assets reported a checksum")
	}
}
