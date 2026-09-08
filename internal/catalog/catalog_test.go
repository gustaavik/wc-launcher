package catalog

import "testing"

// index builds a catalogue from tag/prerelease pairs, newest first.
func index(entries ...Release) Index {
	return Index{SchemaVersion: SchemaVersion, Releases: entries}
}

func release(tag string, prerelease bool) Release {
	return Release{
		Tag:        tag,
		Name:       tag,
		Prerelease: prerelease,
		Assets: []Asset{{
			Name:   "wyvencraft-" + tag + "-aarch64-apple-darwin.tar.gz",
			Path:   "game/" + tag + "/wyvencraft-" + tag + "-aarch64-apple-darwin.tar.gz",
			Size:   1024,
			SHA256: "22d73318f00ca70a57205c32343193fda0607419a37545d60346fc4f19591a34",
		}},
	}
}

// The Latest profile promises the newest *published* build, which is what
// GitHub's own /releases/latest means. A prerelease is opted into by pinning.
func TestLatestSkipsPrereleases(t *testing.T) {
	got, ok := index(release("v0.6.0-rc.1", true), release("v0.5.0", false)).Latest()
	if !ok {
		t.Fatal("Latest found nothing")
	}
	if got.Tag != "v0.5.0" {
		t.Errorf("Latest = %q, want v0.5.0", got.Tag)
	}
}

// Not "the newest anyway": a catalogue of nothing but prereleases has no
// latest, and saying so is what keeps a player off an untested build.
func TestLatestIsEmptyWhenEveryReleaseIsAPrerelease(t *testing.T) {
	if _, ok := index(release("v0.6.0-rc.2", true), release("v0.6.0-rc.1", true)).Latest(); ok {
		t.Error("Latest returned a prerelease")
	}
}

func TestLatestIsEmptyWhenNothingIsPublished(t *testing.T) {
	if _, ok := index().Latest(); ok {
		t.Error("Latest returned something from an empty catalogue")
	}
}

// A pin that has aged out must not silently become "whatever is newest" — that
// would launch a build the profile never asked for.
func TestFindReportsAMissingTagRatherThanTheNewest(t *testing.T) {
	catalogue := index(release("v0.5.0", false), release("v0.4.0", false))

	if _, ok := catalogue.Find("v0.1.0"); ok {
		t.Error("Find invented a release")
	}
	got, ok := catalogue.Find("v0.4.0")
	if !ok {
		t.Fatal("Find missed a release that is present")
	}
	if got.Tag != "v0.4.0" {
		t.Errorf("Find = %q, want v0.4.0", got.Tag)
	}
}

func TestFindOnAnEmptyCatalogueIsNotFound(t *testing.T) {
	if _, ok := index().Find("v0.5.0"); ok {
		t.Error("Find matched in an empty catalogue")
	}
}
