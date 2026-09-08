// Package catalog reads the published game catalogue from object storage.
//
// The game repository is private, so downloads used to be brokered by
// wcauthserver against a server-side GitHub token. They are not any more: the
// release workflow publishes builds and their notes to s3.wyvencraft.com, and
// this package reads them straight from there. No account, no bearer token, no
// account server in the path — so an update works whenever the object store
// does, which is the whole point of moving off the broker.
//
// The catalogue is one object. "Latest" is derived from it rather than stored
// beside it: a second pointer object would open a window where the two
// disagree, and there is nothing it would buy.
package catalog

// SchemaVersion is the index format this launcher understands.
//
// A newer publisher is refused rather than guessed at — a field this launcher
// silently ignores could be the one saying an asset must not be installed.
const SchemaVersion = 1

// Index is the whole published catalogue.
type Index struct {
	SchemaVersion int       `json:"schemaVersion"`
	GeneratedAt   string    `json:"generatedAt,omitempty"`
	Releases      []Release `json:"releases"`
}

// Release is one published game build.
type Release struct {
	Tag         string `json:"tag"`
	Name        string `json:"name,omitempty"`
	PublishedAt string `json:"publishedAt,omitempty"`
	Prerelease  bool   `json:"prerelease,omitempty"`
	// Notes is the release body, Markdown, verbatim. Rendered by
	// internal/markdown rather than in the page: it is not the launcher's own
	// text and the changelog pane inserts the result as HTML.
	Notes  string  `json:"notes,omitempty"`
	Assets []Asset `json:"assets"`
}

// Asset is one downloadable file in a release.
type Asset struct {
	Name string `json:"name"`
	// Path is bucket-relative, never a URL. The client joins it onto its own
	// base, so a mirror works and a tampered catalogue cannot redirect the
	// download to a host of its choosing.
	Path string `json:"path"`
	Size int64  `json:"size"`
	// SHA256 is lowercase hex with no "sha256:" prefix, and is mandatory: the
	// publisher computes it, so there is no unverified case to fall back from.
	SHA256 string `json:"sha256"`
}

// Latest is the newest published release, skipping prereleases.
//
// The same semantics GitHub's /releases/latest has, which is what the Latest
// profile has always meant. A catalogue of nothing but prereleases has no
// latest: saying so keeps a player off an untested build rather than promoting
// one by default.
func (i Index) Latest() (Release, bool) {
	for _, release := range i.Releases {
		if !release.Prerelease {
			return release, true
		}
	}
	return Release{}, false
}

// Find returns the release with this tag.
//
// Deliberately not "or the newest": a pin that has aged out must report as
// missing, or a profile silently starts launching a build it never asked for.
func (i Index) Find(tag string) (Release, bool) {
	if tag == "" {
		return Release{}, false
	}
	for _, release := range i.Releases {
		if release.Tag == tag {
			return release, true
		}
	}
	return Release{}, false
}
