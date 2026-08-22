package services

import (
	"context"
	"errors"
	"log/slog"
	"sync"

	"github.com/gustaavik/wc-launcher/internal/config"
	"github.com/gustaavik/wc-launcher/internal/gamesvc"
	"github.com/gustaavik/wc-launcher/internal/install"
	"github.com/gustaavik/wc-launcher/internal/markdown"
	"github.com/gustaavik/wc-launcher/internal/paths"
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

// UpdateStatus is everything the Play button needs to decide what it says.
type UpdateStatus struct {
	// InstalledTag is "" when nothing is installed yet.
	InstalledTag string `json:"installedTag"`
	// Latest is nil when the check could not be made.
	Latest *ReleaseView `json:"latest"`
	// UpdateAvailable is true when a different version is published.
	UpdateAvailable bool `json:"updateAvailable"`
	// Playable is true when there is something installed to run.
	Playable bool `json:"playable"`
	// Supported is false when the release has no build for this platform.
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
	return core
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
