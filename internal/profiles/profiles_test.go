package profiles

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// store returns an empty store in a temporary directory.
func store(t *testing.T) *Store {
	t.Helper()
	return Open(filepath.Join(t.TempDir(), "profiles.json"))
}

// withFile returns a store over a file containing exactly this text.
func withFile(t *testing.T, contents string) *Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "profiles.json")
	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return Open(path)
}

func TestAMissingProfilesFileYieldsOnlyLatestAndSelectsIt(t *testing.T) {
	s := store(t)

	list := s.List()
	if len(list) != 1 || !list[0].IsLatest() {
		t.Fatalf("want just Latest, got %+v", list)
	}
	if !s.Selected().IsLatest() {
		t.Fatalf("want Latest selected, got %+v", s.Selected())
	}
}

// A launcher that will not start until someone repairs JSON by hand is worse
// than one that offers the built-in profile.
func TestACorruptProfilesFileIsTreatedAsAFirstRunRatherThanFatal(t *testing.T) {
	for _, contents := range []string{"", "{", "null", `{"profiles":"not a list"}`} {
		s := withFile(t, contents)
		if list := s.List(); len(list) != 1 || !list[0].IsLatest() {
			t.Errorf("%q: want just Latest, got %+v", contents, list)
		}
		if !s.Selected().IsLatest() {
			t.Errorf("%q: want Latest selected", contents)
		}
	}
}

func TestLatestCannotBeRenamedRetaggedOrDeleted(t *testing.T) {
	s := store(t)

	for _, tc := range []struct {
		name string
		call func() error
	}{
		{"rename", func() error { _, err := s.Rename(LatestID, "Mine"); return err }},
		{"retag", func() error { _, err := s.Retag(LatestID, "v1"); return err }},
		{"delete", func() error { return s.Delete(LatestID) }},
	} {
		if err := tc.call(); err != ErrLatestIsBuiltIn {
			t.Errorf("%s: want ErrLatestIsBuiltIn, got %v", tc.name, err)
		}
	}
}

func TestSelectingAProfileThatIsGoneFallsBackToLatest(t *testing.T) {
	s := withFile(t, `{"version":1,"selected":"deadbeefdeadbeef","profiles":[]}`)

	if !s.Selected().IsLatest() {
		t.Fatalf("want Latest, got %+v", s.Selected())
	}
	if err := s.Select("nope"); err != ErrNotFound {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestDeletingTheSelectedProfileReselectsLatest(t *testing.T) {
	s := store(t)
	created, err := s.Create("Speedrun", "v0.0.1")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := s.Select(created.ID); err != nil {
		t.Fatalf("select: %v", err)
	}

	if err := s.Delete(created.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if !s.Selected().IsLatest() {
		t.Fatalf("want Latest after deleting the selection, got %+v", s.Selected())
	}
}

func TestAProfileNameMustBeUsableAndNotLatest(t *testing.T) {
	s := store(t)
	if _, err := s.Create("Speedrun", "v0.0.1"); err != nil {
		t.Fatalf("seed: %v", err)
	}

	for _, tc := range []struct {
		name string
		why  string
	}{
		{"", "empty"},
		{"   ", "whitespace only"},
		{strings.Repeat("x", maxNameRunes+1), "too long"},
		{"Latest", "the reserved name"},
		{"latest", "the reserved name in another case"},
		{"LATEST", "the reserved name shouted"},
		{"speedrun", "a duplicate differing only in case"},
	} {
		if _, err := s.Create(tc.name, "v0.0.2"); err == nil {
			t.Errorf("%s should have been refused (%s)", tc.why, tc.name)
		}
	}
}

// An empty tag is exactly what makes a profile Latest, and Latest is
// synthesised rather than stored — so a pinned profile must always name one.
func TestCreateRejectsAnEmptyTag(t *testing.T) {
	s := store(t)
	for _, tag := range []string{"", "   "} {
		if _, err := s.Create("Mine", tag); err == nil {
			t.Errorf("an empty tag (%q) should have been refused", tag)
		}
	}
}

func TestRenamingToItsOwnNameIsNotADuplicate(t *testing.T) {
	s := store(t)
	created, err := s.Create("Speedrun", "v0.0.1")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if _, err := s.Rename(created.ID, "Speedrun"); err != nil {
		t.Fatalf("renaming to the same name should be allowed, got %v", err)
	}
}

// One malformed entry should cost that entry, not the whole list.
func TestUnusableEntriesAreDroppedWithoutLosingTheRest(t *testing.T) {
	s := withFile(t, `{"version":1,"selected":"latest","profiles":[
		{"id":"","name":"no id","tag":"v1"},
		{"id":"aaaa","name":"no tag","tag":""},
		{"id":"latest","name":"impostor","tag":"v1"},
		{"id":"bbbb","name":"keeper","tag":"v0.0.1"},
		{"id":"bbbb","name":"duplicate","tag":"v0.0.2"}
	]}`)

	list := s.List()
	if len(list) != 2 {
		t.Fatalf("want Latest plus one keeper, got %+v", list)
	}
	if list[1].Name != "keeper" {
		t.Fatalf("want the keeper, got %+v", list[1])
	}
}

func TestEveryMutationIsWrittenThroughToDisk(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profiles.json")
	s := Open(path)

	created, err := s.Create("Speedrun", "v0.0.1")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := s.Rename(created.ID, "Speedrun 1.0"); err != nil {
		t.Fatalf("rename: %v", err)
	}
	if _, err := s.Retag(created.ID, "v0.0.2"); err != nil {
		t.Fatalf("retag: %v", err)
	}
	if err := s.Select(created.ID); err != nil {
		t.Fatalf("select: %v", err)
	}

	reopened := Open(path)
	got := reopened.Selected()
	if got.Name != "Speedrun 1.0" || got.Tag != "v0.0.2" || got.ID != created.ID {
		t.Fatalf("reopened store lost the mutations: %+v", got)
	}
}

func TestPinnedTagsReportsEachTagOnceAndExcludesLatest(t *testing.T) {
	s := store(t)
	for _, tc := range []struct{ name, tag string }{
		{"One", "v0.0.1"},
		{"Two", "v0.0.2"},
		{"Also one", "v0.0.1"},
	} {
		if _, err := s.Create(tc.name, tc.tag); err != nil {
			t.Fatalf("create %s: %v", tc.name, err)
		}
	}

	tags := s.PinnedTags()
	if len(tags) != 2 {
		t.Fatalf("want two distinct tags, got %v", tags)
	}
	for _, tag := range tags {
		if tag == "" {
			t.Fatal("Latest has no tag and must not appear in PinnedTags")
		}
	}
}

// Deleting a profile must not delete the build: a deletion is cheap to regret,
// a few hundred megabytes of re-download is not.
func TestDeletingAProfileOnlyUnpinsItsTag(t *testing.T) {
	s := store(t)
	created, err := s.Create("Speedrun", "v0.0.1")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := s.Delete(created.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if tags := s.PinnedTags(); len(tags) != 0 {
		t.Fatalf("want no pins after deletion, got %v", tags)
	}
}
