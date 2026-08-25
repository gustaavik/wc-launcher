package install

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gustaavik/wc-launcher/internal/paths"
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

// ---------------------------------------------------------- builds and prune

// installed fakes an unpacked build: a version directory with a game binary,
// and a tag marker unless one is deliberately withheld.
func installed(t *testing.T, layout paths.Layout, tag string, marker bool) {
	t.Helper()
	dir := layout.VersionDir(tag)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, GameBinary()), []byte("#!/bin/true\n"), 0o755); err != nil {
		t.Fatalf("write binary: %v", err)
	}
	if marker {
		if err := os.WriteFile(filepath.Join(dir, tagMarker), []byte(tag+"\n"), 0o644); err != nil {
			t.Fatalf("write marker: %v", err)
		}
	}
	// Distinct mtimes, so "newest first" is well defined rather than resolution-dependent.
	stamp := time.Now().Add(-time.Duration(len(tag)) * time.Hour)
	if err := os.Chtimes(dir, stamp, stamp); err != nil {
		t.Fatalf("stamp %s: %v", dir, err)
	}
}

// testLayout is a Layout under a temporary directory.
func testLayout(t *testing.T) paths.Layout {
	t.Helper()
	root := t.TempDir()
	layout := paths.Layout{
		Root:     root,
		Versions: filepath.Join(root, "versions"),
		Data:     filepath.Join(root, "data"),
		Logs:     filepath.Join(root, "logs"),
	}
	if err := os.MkdirAll(layout.Versions, 0o755); err != nil {
		t.Fatalf("create versions: %v", err)
	}
	return layout
}

func tagsOf(builds []Build) []string {
	out := make([]string, 0, len(builds))
	for _, build := range builds {
		out = append(out, build.Tag)
	}
	return out
}

func TestListReportsEveryUnpackedBuildNewestFirst(t *testing.T) {
	layout := testLayout(t)
	// The helper stamps by name length, so the shortest tag is the newest.
	for _, tag := range []string{"v3", "v0.2", "v0.0.1"} {
		installed(t, layout, tag, true)
	}
	i := New(layout, nil)

	if got := tagsOf(i.List()); !reflect.DeepEqual(got, []string{"v3", "v0.2", "v0.0.1"}) {
		t.Fatalf("want newest first, got %v", got)
	}
}

// safeTag is lossy, so the directory name cannot be trusted to reproduce a tag.
// The marker is what makes the round trip work.
func TestListReadsTheTagFromTheMarkerRatherThanTheDirectoryName(t *testing.T) {
	layout := testLayout(t)
	const tag = "v1.0/beta" // safeTag mangles the slash
	installed(t, layout, tag, true)

	got := tagsOf(New(layout, nil).List())
	if len(got) != 1 || got[0] != tag {
		t.Fatalf("want the real tag %q back, got %v", tag, got)
	}
}

// A build installed before the marker existed still has to be recognised. The
// directory name is right in practice: safeTag is the identity on a real tag.
func TestListFallsBackToTheDirectoryNameForABuildInstalledBeforeTheMarker(t *testing.T) {
	layout := testLayout(t)
	installed(t, layout, "v0.0.3", false)

	got := tagsOf(New(layout, nil).List())
	if len(got) != 1 || got[0] != "v0.0.3" {
		t.Fatalf("want the legacy build recognised, got %v", got)
	}
}

func TestListIgnoresStagingDirectoriesPartialDownloadsAndEmptyDirectories(t *testing.T) {
	layout := testLayout(t)
	installed(t, layout, "v0.0.1", true)

	for _, name := range []string{".staging-v0.0.2", ".download-thing.part", "not-a-build"} {
		if err := os.MkdirAll(filepath.Join(layout.Versions, name), 0o755); err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
	}

	if got := tagsOf(New(layout, nil).List()); !reflect.DeepEqual(got, []string{"v0.0.1"}) {
		t.Fatalf("want only the real build, got %v", got)
	}
}

// The invariant the whole profile system rests on: a pinned build is not an old
// build, it is the build that profile *is*.
func TestPruneNeverDeletesAKeptTag(t *testing.T) {
	for _, tc := range []struct {
		name    string
		keep    []string
		spare   int
		survive []string
	}{
		{
			name:    "a pin survives even with no spare budget at all",
			keep:    []string{"v0.0.1"},
			spare:   0,
			survive: []string{"v0.0.1"},
		},
		{
			name:    "several pins all survive",
			keep:    []string{"v0.0.1", "v0.0.3"},
			spare:   0,
			survive: []string{"v0.0.1", "v0.0.3"},
		},
		{
			name:    "a pin survives alongside the spare window",
			keep:    []string{"v0.0.1"},
			spare:   keptVersions,
			survive: []string{"v0.0.1", "v0.0.4", "v0.0.3"},
		},
		{
			name:    "an empty keep set is not a keep-everything set",
			keep:    nil,
			spare:   1,
			survive: []string{"v0.0.4"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			layout := testLayout(t)
			// Installed oldest first, restamped as we go, so "newest" means
			// the highest version rather than whatever order ReadDir returns.
			for _, tag := range []string{"v0.0.1", "v0.0.2", "v0.0.3", "v0.0.4"} {
				installed(t, layout, tag, true)
				time.Sleep(time.Millisecond)
				now := time.Now()
				_ = os.Chtimes(layout.VersionDir(tag), now, now)
			}
			i := New(layout, nil)

			i.Prune(tc.keep, tc.spare)

			for _, tag := range tc.survive {
				if !i.Installed(tag) {
					t.Errorf("%s should have survived the prune", tag)
				}
			}
		})
	}
}

func TestPruneKeepsTheSpareMostRecentBuilds(t *testing.T) {
	layout := testLayout(t)
	for _, tag := range []string{"v0.0.1", "v0.0.2", "v0.0.3"} {
		installed(t, layout, tag, true)
		time.Sleep(time.Millisecond)
		now := time.Now()
		_ = os.Chtimes(layout.VersionDir(tag), now, now)
	}
	i := New(layout, nil)

	removed := i.Prune(nil, 2)

	if !reflect.DeepEqual(removed, []string{"v0.0.1"}) {
		t.Fatalf("want the oldest dropped, got %v", removed)
	}
	if got := tagsOf(i.List()); !reflect.DeepEqual(got, []string{"v0.0.3", "v0.0.2"}) {
		t.Fatalf("want the two newest kept, got %v", got)
	}
}

func TestRemoveIsANoOpForABuildThatIsNotInstalled(t *testing.T) {
	i := New(testLayout(t), nil)

	if err := i.Remove("v9.9.9"); err != nil {
		t.Fatalf("removing an absent build should be a no-op, got %v", err)
	}
	if err := i.Remove(""); err != nil {
		t.Fatalf("removing nothing should be a no-op, got %v", err)
	}
}
