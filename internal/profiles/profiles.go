// Package profiles is the player's list of launcher profiles: a name bound to a
// Wyvencraft version, and which of them is selected.
//
// Not to be confused with internal/profile, which owns the game's profile.toml
// — the session handoff. A *launcher* profile chooses which build runs; a
// *game* profile is a signed-in identity. The two never meet, and no file
// imports both.
//
// One profile is not stored at all. Latest is synthesised on every read, so
// "file missing", "file empty" and "file corrupt" all mean exactly the same
// thing: you have Latest, and it is selected. That is the correct first-run
// state, and it removes the branch where a hand-edited file leaves a launcher
// with no default profile.
package profiles

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// LatestID is the built-in profile's id.
//
// Reserved: generated ids are sixteen hex characters, so nothing can collide
// with it.
const LatestID = "latest"

// LatestName is what the built-in profile is called.
const LatestName = "Latest"

// maxNameRunes bounds a profile name. Long enough for anything descriptive,
// short enough to fit the sidebar picker without truncation doing the deciding.
const maxNameRunes = 40

// Profile is one entry in the player's list.
type Profile struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Tag is the release this profile is pinned to. Empty only for Latest,
	// which follows whatever is newest rather than pinning anything.
	Tag string `json:"tag"`
	// CreatedAt orders the list under Latest. Zero for Latest itself.
	CreatedAt time.Time `json:"createdAt"`
}

// IsLatest reports whether this is the built-in always-newest profile.
func (p Profile) IsLatest() bool { return p.ID == LatestID }

// latest is the synthesised built-in profile.
func latest() Profile {
	return Profile{ID: LatestID, Name: LatestName}
}

// Set is the contents of profiles.json: the pinned profiles, and the selection.
type Set struct {
	// Version is the file format, for a future migration to branch on. Absent
	// in a file written before it existed, which reads as 0 and is fine.
	Version int `json:"version"`
	// Selected is a profile id, or LatestID. An id that is not present falls
	// back to Latest on read.
	Selected string `json:"selected"`
	// Profiles holds only pinned profiles. Latest is never written.
	Profiles []Profile `json:"profiles"`
}

// currentVersion is stamped into every file this package writes.
const currentVersion = 1

// ErrLatestIsBuiltIn is returned by any mutator aimed at Latest.
var ErrLatestIsBuiltIn = errors.New("Latest is the built-in profile and cannot be changed or removed")

// ErrNotFound is returned when no profile has the given id.
var ErrNotFound = errors.New("no such profile")

// Store is the profile list, kept in step with profiles.json.
//
// Every mutator persists while holding the lock, so there is no separate Save
// to forget and no window in which memory and disk disagree about what would be
// launched.
type Store struct {
	mu   sync.Mutex
	path string
	set  Set
}

// Open reads the store at path.
//
// Never fails, following config.Load: a missing file is a first run, and a
// corrupt one is a first run too. Refusing to start until someone repairs JSON
// by hand would be a worse answer than a list containing Latest.
func Open(path string) *Store {
	return &Store{path: path, set: load(path)}
}

// load parses profiles.json, discarding anything unusable.
//
// Entries are dropped individually rather than failing the file: one malformed
// profile should cost that profile, not the rest of them.
func load(path string) Set {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Set{}
	}
	var set Set
	if err := json.Unmarshal(raw, &set); err != nil {
		return Set{}
	}

	seen := make(map[string]bool, len(set.Profiles))
	kept := make([]Profile, 0, len(set.Profiles))
	for _, profile := range set.Profiles {
		// A profile with no id cannot be selected or deleted, and one with no
		// tag is indistinguishable from Latest — which is synthesised, not
		// stored. Neither is recoverable, so neither is kept.
		if profile.ID == "" || profile.Tag == "" || profile.ID == LatestID {
			continue
		}
		if seen[profile.ID] {
			continue
		}
		seen[profile.ID] = true
		kept = append(kept, profile)
	}
	set.Profiles = kept

	if set.Selected != LatestID && !seen[set.Selected] {
		set.Selected = LatestID
	}
	return set
}

// List is every profile, Latest first and the rest in creation order.
//
// Latest leads because it is the default and the one most sessions want; the
// rest are stable so the picker does not reorder itself under the cursor.
func (s *Store) List() []Profile {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]Profile, 0, len(s.set.Profiles)+1)
	out = append(out, latest())
	out = append(out, s.set.Profiles...)
	return out
}

// Selected is the profile that would be launched.
func (s *Store) Selected() Profile {
	s.mu.Lock()
	defer s.mu.Unlock()

	if profile, ok := s.find(s.set.Selected); ok {
		return profile
	}
	return latest()
}

// Find looks a profile up by id. Latest is findable by LatestID.
func (s *Store) Find(id string) (Profile, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.find(id)
}

// find resolves an id. Caller holds the lock.
func (s *Store) find(id string) (Profile, bool) {
	if id == LatestID {
		return latest(), true
	}
	for _, profile := range s.set.Profiles {
		if profile.ID == id {
			return profile, true
		}
	}
	return Profile{}, false
}

// Select makes id the profile that will be launched.
func (s *Store) Select(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.find(id); !ok {
		return ErrNotFound
	}
	s.set.Selected = id
	return s.save()
}

// Create adds a profile pinned to tag.
func (s *Store) Create(name, tag string) (Profile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	name, err := s.validName(name, "")
	if err != nil {
		return Profile{}, err
	}
	tag = strings.TrimSpace(tag)
	if tag == "" {
		// An empty tag is what makes a profile Latest, and there is exactly one
		// of those.
		return Profile{}, errors.New("pick a version for this profile")
	}

	profile := Profile{ID: newID(), Name: name, Tag: tag, CreatedAt: time.Now().UTC()}
	s.set.Profiles = append(s.set.Profiles, profile)
	if err := s.save(); err != nil {
		return Profile{}, err
	}
	return profile, nil
}

// Rename changes a profile's name.
func (s *Store) Rename(id, name string) (Profile, error) {
	return s.mutate(id, func(profile *Profile) error {
		clean, err := s.validName(name, id)
		if err != nil {
			return err
		}
		profile.Name = clean
		return nil
	})
}

// Retag repins a profile to a different release.
func (s *Store) Retag(id, tag string) (Profile, error) {
	return s.mutate(id, func(profile *Profile) error {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			return errors.New("pick a version for this profile")
		}
		profile.Tag = tag
		return nil
	})
}

// mutate applies change to the profile with this id and persists the result.
func (s *Store) mutate(id string, change func(*Profile) error) (Profile, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if id == LatestID {
		return Profile{}, ErrLatestIsBuiltIn
	}
	for i := range s.set.Profiles {
		if s.set.Profiles[i].ID != id {
			continue
		}
		// Applied to a copy so a rejected change cannot leave the store holding
		// a half-applied profile.
		updated := s.set.Profiles[i]
		if err := change(&updated); err != nil {
			return Profile{}, err
		}
		s.set.Profiles[i] = updated
		if err := s.save(); err != nil {
			return Profile{}, err
		}
		return updated, nil
	}
	return Profile{}, ErrNotFound
}

// Delete removes a profile.
//
// The build it was pinned to is deliberately left on disk: deleting a profile
// is cheap to regret and re-downloading a few hundred megabytes is not. The tag
// simply stops appearing in PinnedTags, which is what lets a later prune
// reclaim it.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if id == LatestID {
		return ErrLatestIsBuiltIn
	}
	for i, profile := range s.set.Profiles {
		if profile.ID != id {
			continue
		}
		s.set.Profiles = append(s.set.Profiles[:i], s.set.Profiles[i+1:]...)
		if s.set.Selected == id {
			s.set.Selected = LatestID
		}
		return s.save()
	}
	return ErrNotFound
}

// PinnedTags is every release some profile is pinned to, each once.
//
// This is the set an install prune must never delete: the version a profile is
// pinned to is not an old build, it is the build that profile *is*.
func (s *Store) PinnedTags() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	seen := make(map[string]bool, len(s.set.Profiles))
	tags := make([]string, 0, len(s.set.Profiles))
	for _, profile := range s.set.Profiles {
		if seen[profile.Tag] {
			continue
		}
		seen[profile.Tag] = true
		tags = append(tags, profile.Tag)
	}
	return tags
}

// validName checks a proposed name, ignoring the profile it belongs to so a
// rename to the same name is not a duplicate. Caller holds the lock.
func (s *Store) validName(name, ownID string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", errors.New("give this profile a name")
	}
	if len([]rune(name)) > maxNameRunes {
		return "", fmt.Errorf("a profile name is at most %d characters", maxNameRunes)
	}
	if strings.EqualFold(name, LatestName) {
		return "", fmt.Errorf("%q is the built-in profile that always runs the newest build; pick another name", LatestName)
	}
	for _, profile := range s.set.Profiles {
		if profile.ID != ownID && strings.EqualFold(profile.Name, name) {
			return "", fmt.Errorf("there is already a profile called %q", profile.Name)
		}
	}
	return name, nil
}

// save writes the file. Caller holds the lock.
func (s *Store) save() error {
	s.set.Version = currentVersion
	raw, err := json.MarshalIndent(s.set, "", "  ")
	if err != nil {
		return fmt.Errorf("encode profiles: %w", err)
	}
	return writeAtomic(s.path, raw)
}

// writeAtomic replaces a file in one step, so a reader sees either the old
// contents or the new ones.
//
// This file decides what gets launched, and a half-written one would read back
// as "you have no profiles" — which is indistinguishable from a first run and
// would silently drop the player's list.
func writeAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}

	temp, err := os.CreateTemp(dir, ".profiles-*.json")
	if err != nil {
		return fmt.Errorf("create a temporary file in %s: %w", dir, err)
	}
	name := temp.Name()
	defer os.Remove(name)

	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return fmt.Errorf("write %s: %w", name, err)
	}
	// Flushed before the rename: a rename that lands ahead of the bytes would
	// survive a crash as an empty file, which is the failure this avoids.
	if err := temp.Sync(); err != nil {
		temp.Close()
		return fmt.Errorf("flush %s: %w", name, err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close %s: %w", name, err)
	}
	if err := os.Rename(name, path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
}

// newID is a profile's stable identity.
//
// Random rather than derived from the name, because a profile can be renamed
// and a rename must not orphan the selection or the pins that point at it.
func newID() string {
	var raw [8]byte
	if _, err := rand.Read(raw[:]); err != nil {
		// crypto/rand does not fail in practice, and a profile id is not a
		// secret: a timestamp is a fine identity of last resort.
		return fmt.Sprintf("%016x", time.Now().UnixNano())
	}
	return hex.EncodeToString(raw[:])
}
