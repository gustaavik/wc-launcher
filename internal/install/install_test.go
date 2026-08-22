package install

import (
	"runtime"
	"strings"
	"testing"

	"github.com/gustaavik/wc-launcher/internal/wcauth"
)

// The real v0.0.1 asset list, so the matcher is exercised against what the
// release workflow actually publishes — including the .sha256 siblings it must
// not mistake for archives.
func realRelease() wcauth.Release {
	return wcauth.Release{
		Tag: "v0.0.1",
		Assets: []wcauth.Asset{
			{ID: "521286005", Name: "wyvencraft-v0.0.1-aarch64-apple-darwin.tar.gz", Size: "6708880", Digest: "sha256:5129"},
			{ID: "521286006", Name: "wyvencraft-v0.0.1-aarch64-apple-darwin.tar.gz.sha256", Size: "112"},
			{ID: "521289567", Name: "wyvencraft-v0.0.1-x86_64-apple-darwin.tar.gz", Size: "6967744", Digest: "sha256:98d4"},
			{ID: "521289571", Name: "wyvencraft-v0.0.1-x86_64-apple-darwin.tar.gz.sha256", Size: "111"},
		},
	}
}

func TestSelectAssetPicksThisPlatformsArchive(t *testing.T) {
	asset, err := SelectAsset(realRelease())

	switch runtime.GOOS + "/" + runtime.GOARCH {
	case "darwin/arm64":
		if err != nil {
			t.Fatalf("SelectAsset: %v", err)
		}
		if asset.Name != "wyvencraft-v0.0.1-aarch64-apple-darwin.tar.gz" {
			t.Errorf("picked %q", asset.Name)
		}
	case "darwin/amd64":
		if err != nil {
			t.Fatalf("SelectAsset: %v", err)
		}
		if asset.Name != "wyvencraft-v0.0.1-x86_64-apple-darwin.tar.gz" {
			t.Errorf("picked %q", asset.Name)
		}
	default:
		// No Linux or Windows asset in this release.
		if err == nil {
			t.Errorf("want no build for %s, got %q", runtime.GOOS, asset.Name)
		}
	}
}

// The .sha256 files also contain the target triple. Matching on the triple
// alone would pick a 112-byte text file and try to unpack it.
func TestSelectAssetNeverPicksAChecksumSibling(t *testing.T) {
	asset, err := SelectAsset(realRelease())
	if err != nil {
		t.Skipf("no build for %s/%s", runtime.GOOS, runtime.GOARCH)
	}
	if strings.HasSuffix(asset.Name, ".sha256") {
		t.Errorf("picked the checksum file %q", asset.Name)
	}
}

// A release with no build for this platform must say so, rather than failing
// later as a download error — retrying will never help.
func TestAReleaseWithNoBuildForThisPlatformIsReportedClearly(t *testing.T) {
	release := wcauth.Release{Tag: "v9.9.9", Assets: []wcauth.Asset{
		{ID: "1", Name: "wyvencraft-v9.9.9-riscv64-unknown-linux-gnu.tar.gz"},
	}}

	_, err := SelectAsset(release)
	var noBuild *ErrNoBuild
	if !asNoBuild(err, &noBuild) {
		t.Fatalf("want *ErrNoBuild, got %T: %v", err, err)
	}
	if !strings.Contains(noBuild.Error(), "v9.9.9") {
		t.Errorf("error should name the tag: %v", noBuild)
	}
}

func TestChecksumAssetFindsTheSibling(t *testing.T) {
	release := realRelease()
	archive := release.Assets[0]

	sibling, ok := ChecksumAsset(release, archive)
	if !ok {
		t.Fatal("sibling not found")
	}
	if sibling.Name != archive.Name+".sha256" {
		t.Errorf("found %q", sibling.Name)
	}

	if _, ok := ChecksumAsset(wcauth.Release{}, archive); ok {
		t.Error("an empty release should have no sibling")
	}
}

func TestParseChecksumFileReadsWhatShasumWrites(t *testing.T) {
	const hash = "51295ab76d3e630c3efbc54e086203712c3cc76c80965ed9cbe90326336836d5"
	line := hash + "  wyvencraft-v0.0.1-aarch64-apple-darwin.tar.gz\n"

	if got := ParseChecksum(line); got != hash {
		t.Errorf("parseChecksumFile = %q, want %q", got, hash)
	}
	// Uppercase is still valid hex.
	if got := ParseChecksum(strings.ToUpper(hash) + "  file"); got != hash {
		t.Errorf("uppercase hash = %q", got)
	}
}

// Garbage must produce "" so the caller refuses to install, rather than a value
// that happens to compare unequal and produces a confusing mismatch message.
func TestParseChecksumFileRejectsAnythingThatIsNotAHash(t *testing.T) {
	for _, input := range []string{"", "   ", "not-a-hash  file", "deadbeef  file", "zz" + strings.Repeat("0", 62)} {
		if got := ParseChecksum(input); got != "" {
			t.Errorf("ParseChecksum(%q) = %q, want empty", input, got)
		}
	}
}

func TestGameBinaryHasThePlatformsExtension(t *testing.T) {
	want := "wyvencraft"
	if runtime.GOOS == "windows" {
		want = "wyvencraft.exe"
	}
	if got := GameBinary(); got != want {
		t.Errorf("GameBinary = %q, want %q", got, want)
	}
}

func asNoBuild(err error, target **ErrNoBuild) bool {
	e, ok := err.(*ErrNoBuild)
	if ok {
		*target = e
	}
	return ok
}
