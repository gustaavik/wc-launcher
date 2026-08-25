package services

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"sync"

	"github.com/gustaavik/wc-launcher/internal/config"
	"github.com/gustaavik/wc-launcher/internal/gamesvc"
	"github.com/gustaavik/wc-launcher/internal/install"
	"github.com/gustaavik/wc-launcher/internal/markdown"
	"github.com/gustaavik/wc-launcher/internal/paths"
	"github.com/gustaavik/wc-launcher/internal/profiles"
	"github.com/gustaavik/wc-launcher/internal/selfupdate"
	"github.com/gustaavik/wc-launcher/internal/wcauth"
)

// Emitter publishes an event to the frontend. Satisfied by the Wails app; a
// no-op in tests.
type Emitter interface {
	Emit(name string, data any)
}

// AccountView is what the UI knows about the signed-in player.
type AccountView struct {
	ID       string `json:"id"`
	Username string `json:"username"`
}

// ReleaseView is a release, ready to display.
type ReleaseView struct {
	Tag         string `json:"tag"`
	Name        string `json:"name"`
	PublishedAt string `json:"publishedAt"`
	Prerelease  bool   `json:"prerelease"`
	// NotesHTML is rendered server-side; the notes are not the launcher's own
	// text, so they never reach the page as Markdown to be parsed there.
	NotesHTML string `json:"notesHtml"`
}

// ProfileView is one launcher profile, ready to display.
type ProfileView struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Tag is the release this profile is pinned to. Empty for Latest, which
	// follows whatever is newest rather than pinning anything.
	Tag string `json:"tag"`
	// Latest is true for the built-in profile: it cannot be renamed, retagged
	// or deleted, and it forces an update when a newer build is published.
	Latest bool `json:"latest"`
	// Installed is true when this profile has a build on disk it could run.
	Installed bool `json:"installed"`
}

// ReleaseOption is one entry in the version picker.
//
// Deliberately without rendered notes: the picker shows a list, and rendering
// thirty changelogs to fill a dropdown is work nobody asked for.
type ReleaseOption struct {
	Tag         string `json:"tag"`
	Name        string `json:"name"`
	PublishedAt string `json:"publishedAt"`
	Prerelease  bool   `json:"prerelease"`
	// Supported is false when this release publishes no build for this
	// platform, so the picker can grey it out rather than offer a pin that can
	// never install.
	Supported bool `json:"supported"`
	Installed bool `json:"installed"`
}

// UpdateStatus is everything the Play button needs to decide what it says.
//
// The truth table it has to produce:
//
//	profile     on disk   published   result
//	Latest      —         v4          Install
//	Latest      v4        v4          Play
//	Latest      v3        v4          Update & Play   (Required)
//	Latest      v3        unknown     Play            (offline: never forced)
//	pinned v2   v2        v4          Play
//	pinned v2   —         v4          Install
type UpdateStatus struct {
	// Profile is the selection, so the UI never has to join two calls.
	Profile ProfileView `json:"profile"`
	// InstalledTag is the build the selected profile would launch, "" when it
	// has none. For Latest that is the newest build on disk; for a pinned
	// profile it is its own tag, or "" if that build is not installed.
	InstalledTag string `json:"installedTag"`
	// Latest is the newest published release, nil when the check could not be
	// made. Reported even for a pinned profile: the sidebar shows it.
	Latest *ReleaseView `json:"latest"`
	// Target is the release the selected profile wants. Nil when it could not
	// be resolved. Carries the notes, so the changelog pane can show the
	// profile's own changelog rather than always the newest one.
	Target *ReleaseView `json:"target"`
	// UpdateAvailable is true when Target is published and not installed.
	UpdateAvailable bool `json:"updateAvailable"`
	// Required is true when the update cannot be deferred: the Latest profile
	// is a promise to run the newest build, so an older one is not a fallback
	// the player gets to choose.
	//
	// False whenever the newest release is unknown. An update that cannot be
	// checked for must not become a lockout — a player who cannot reach the
	// account server still gets to play the build they have.
	Required bool `json:"required"`
	// Playable is true when there is a build this profile may run right now.
	// False under a required update, which is what makes the force real.
	Playable bool `json:"playable"`
	// Supported is false when Target has no build for this platform.
	Supported bool `json:"supported"`
	// Message explains anything the fields above cannot.
	Message string `json:"message"`
}

// GameStatus mirrors the runner's view of the process.
type GameStatus = gamesvc.Status

// Core holds the shared state the three services sit on.
type Core struct {
	Layout   paths.Layout
	Emitter  Emitter
	Session  *Session
	Runner   *gamesvc.Runner
	Install  *install.Installer
	Profiles *profiles.Store
	Client   *wcauth.Client
	Launcher *selfupdate.Client
	Settings config.Settings
	// Quit shuts the launcher down. Set by main once the app exists; nil in
	// tests. Applying a launcher update is the only thing that needs it, and
	// routing it through a func is what keeps Wails out of this package.
	Quit func()

	mu sync.Mutex
	// installing guards against a second Install while one is in flight, and
	// carries the cancel func for the running one.
	cancelInstall context.CancelFunc
	// cancelLauncher is the same for a launcher self-update. Separate, because
	// the two are unrelated downloads and cancelling one must not stop the
	// other.
	cancelLauncher context.CancelFunc
	// stagedLauncher is the unpacked launcher build waiting for a restart.
	stagedLauncher string
	// latest is the newest release from the last successful check, and the
	// clock the Latest profile is measured against. Nil until one succeeds,
	// which is exactly what keeps an offline launcher playable rather than
	// locked behind an update it cannot see.
	latest *wcauth.Release
}

// knownLatest is the newest release the launcher has actually seen.
//
// The second return distinguishes "we know, and it is this" from "we have not
// been able to look" — a distinction the forced update depends on.
func (c *Core) knownLatest() (wcauth.Release, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.latest == nil {
		return wcauth.Release{}, false
	}
	return *c.latest, true
}

// setKnownLatest records, or with nil forgets, the newest release.
func (c *Core) setKnownLatest(release *wcauth.Release) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.latest = release
}

// NewCore wires everything together.
func NewCore(layout paths.Layout, emitter Emitter) *Core {
	settings := config.Load(layout.SettingsFile())
	client := wcauth.New(settings.ResolvedAuthURL())
	runner := gamesvc.NewRunner()

	core := &Core{
		Layout:   layout,
		Emitter:  emitter,
		Runner:   runner,
		Client:   client,
		Launcher: selfupdate.NewClient(""),
		Settings: settings,
	}
	core.Session = NewSession(layout, client, runner.Running)
	core.Install = install.New(layout, client)
	core.Profiles = profiles.Open(layout.ProfilesFile())
	migrate(layout)
	return core
}

// migrate clears state the launcher no longer keeps.
//
// installed.json recorded the single installed build, from before profiles made
// "which build" a per-profile question. Nothing reads it any more: versions/ is
// the only record, and unlike a file it cannot go stale.
func migrate(layout paths.Layout) {
	if err := os.Remove(layout.StateFile()); err != nil && !os.IsNotExist(err) {
		slog.Warn("could not remove the old install record", "error", err)
	}
}

// toProfileView renders a profile for the UI, answering "could this run right
// now" against what is actually on disk.
func toProfileView(profile profiles.Profile, installer *install.Installer) ProfileView {
	view := ProfileView{
		ID:     profile.ID,
		Name:   profile.Name,
		Tag:    profile.Tag,
		Latest: profile.IsLatest(),
	}
	if profile.IsLatest() {
		view.Installed = len(installer.List()) > 0
	} else {
		view.Installed = installer.Installed(profile.Tag)
	}
	return view
}

// newInstaller rebuilds the installer against the Core's current client.
// Needed when the account server changes: the installer resolves download URLs
// through it.
func newInstaller(c *Core) *install.Installer {
	return install.New(c.Layout, c.Client)
}

func (c *Core) emit(name string, data any) {
	if c.Emitter != nil {
		c.Emitter.Emit(name, data)
	}
}

func toAccountView(account *wcauth.Account) *AccountView {
	if account == nil {
		return nil
	}
	return &AccountView{ID: account.ID, Username: account.Username}
}

func toReleaseView(release wcauth.Release) ReleaseView {
	return ReleaseView{
		Tag:         release.Tag,
		Name:        release.Name,
		PublishedAt: release.PublishedAt,
		Prerelease:  release.Prerelease,
		NotesHTML:   markdown.Render(release.Notes),
	}
}

// userMessage turns an error into something worth showing a player.
//
// The distinction that matters is refusal versus outage: one is the player's
// problem to fix, the other is not their problem at all.
func userMessage(err error) string {
	if err == nil {
		return ""
	}
	if wcauth.Unreachable(err) {
		return "Could not reach the account server. Check your connection and try again."
	}
	var refused *wcauth.Error
	if errors.As(err, &refused) {
		return refused.Error()
	}
	if selfupdate.Unreachable(err) {
		return "Could not reach GitHub. Check your connection and try again."
	}
	var github *selfupdate.Error
	if errors.As(err, &github) {
		return github.Error()
	}
	return err.Error()
}

func logIfErr(message string, err error) {
	if err != nil {
		slog.Warn(message, "error", err)
	}
}
