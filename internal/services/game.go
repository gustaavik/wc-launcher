package services

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/gustaavik/wc-launcher/internal/deps"
	"github.com/gustaavik/wc-launcher/internal/gamesvc"
	"github.com/gustaavik/wc-launcher/internal/install"
)

// devGameDirVar points the launcher at a locally built game instead of a
// downloaded one.
//
// Exists because the two ends of this change land at different times: a release
// published before the game learned about WYVEN_DATA_DIR cannot be used to test
// that it works. Set this to a `cargo build --release` output directory and the
// whole launch path — profile handoff, environment, working directory — is
// exercised against it.
const devGameDirVar = "WCL_DEV_GAME_DIR"

// GameService starts and stops the game.
type GameService struct{ core *Core }

func NewGameService(core *Core) *GameService { return &GameService{core: core} }

// Launch starts the game.
//
// Returns an empty string on success. The ordering inside is load-bearing and
// documented step by step below.
func (g *GameService) Launch() string {
	if g.core.Runner.Running() {
		return gamesvc.ErrAlreadyRunning.Error()
	}

	versionDir, err := g.versionDir()
	if err != nil {
		return err.Error()
	}

	ctx := context.Background()

	// 1. Refresh if the token is close to expiring, and persist the rotation.
	//    Done *before* the game starts, because once it is running the launcher
	//    must not touch the token family at all.
	if _, err := g.core.Session.AccessToken(ctx); err != nil {
		if !errors.Is(err, ErrSignedOut) {
			return userMessage(err)
		}
		// Signed out is allowed: the game runs offline, singleplayer only.
	}

	// 2. Make sure the keys are cached, so hosting works. Best-effort — a
	//    stale-but-valid authkeys.toml is better than none, and singleplayer
	//    does not need it at all.
	if g.core.Session.Account() != nil {
		logIfErr("could not cache the auth keys", g.core.Session.CacheKeys(ctx))
	}

	// 3. profile.toml is already current: AccountToken persisted it above, and
	//    Session writes it on every adopt. Nothing more to hand over.

	// 4. Make sure there is a Vulkan driver. Normally installed alongside the
	//    game, so this is a stat; the download is the repair path for a build
	//    installed before this existed, a pinned profile, or WCL_DEV_GAME_DIR.
	//    Runner.Start refuses if it is still missing afterwards.
	moltenVK := g.core.Layout.MoltenVKDir(deps.Version)
	if !gamesvc.VulkanReady(versionDir, moltenVK) {
		logIfErr("could not install the graphics driver", g.installDriver(ctx))
	}

	opts := gamesvc.Options{
		DataDir:     g.core.Layout.Data,
		AuthURL:     g.core.Settings.ResolvedAuthURL(),
		VersionDir:  versionDir,
		LogFilter:   g.core.Settings.LogFilter,
		MoltenVKDir: moltenVK,
	}

	err = g.core.Runner.Start(opts, g.core.Layout.GameLog(),
		func(line string) { g.core.emit("game:log", line) },
		func(status gamesvc.Status) {
			g.core.emit("game:state", status)
			// 4. The game rotates the refresh token while it runs, so what the
			//    launcher holds is now stale and the file is authoritative.
			g.core.Session.Reload(context.Background())
			g.core.emit("auth:changed", toAccountView(g.core.Session.Account()))
		})
	if err != nil {
		return err.Error()
	}

	g.core.emit("game:state", g.core.Runner.Status())
	return ""
}

// installDriver fetches the Vulkan driver, reporting onto the event the update
// panel already renders.
//
// It registers its cancel function where UpdateService.Cancel looks, because
// the panel it raises has a Cancel button on it: a slow connection must not
// leave the player watching a bar they cannot stop. That also means an install
// cannot start underneath this, which is what we want — the same event stream
// would otherwise be describing two downloads at once.
func (g *GameService) installDriver(ctx context.Context) error {
	g.core.mu.Lock()
	if g.core.cancelInstall != nil {
		g.core.mu.Unlock()
		return errors.New("an install is already running")
	}
	ctx, cancel := context.WithCancel(ctx)
	g.core.cancelInstall = cancel
	g.core.mu.Unlock()

	defer func() {
		g.core.mu.Lock()
		g.core.cancelInstall = nil
		g.core.mu.Unlock()
		cancel()
	}()

	_, err := deps.Ensure(ctx, g.core.Layout, func(p install.Progress) {
		g.core.emit("update:progress", p)
	})

	// The panel was raised by the first progress event and nothing else will
	// lower it, so this step has to say when it is over.
	if err != nil {
		g.core.emit("update:progress", install.Progress{Phase: "failed", Percent: -1})
		return err
	}
	g.core.emit("update:progress", install.Progress{Phase: "done", Percent: 100})
	return nil
}

// Stop ends the game.
func (g *GameService) Stop() string {
	if err := g.core.Runner.Stop(); err != nil {
		return err.Error()
	}
	return ""
}

// Status reports what the game process is doing.
func (g *GameService) Status() GameStatus { return g.core.Runner.Status() }

// LogPath is where the last run's output was written, for a "reveal" button.
func (g *GameService) LogPath() string { return g.core.Layout.GameLog() }

// versionDir is the install to launch: the dev override if set, otherwise
// whatever the selected profile resolves to.
func (g *GameService) versionDir() (string, error) {
	if dev := strings.TrimSpace(os.Getenv(devGameDirVar)); dev != "" {
		if _, err := os.Stat(dev); err != nil {
			return "", errors.New(devGameDirVar + " is set but " + dev + " does not exist")
		}
		return dev, nil
	}
	return g.launchPlan()
}

// launchPlan is the build the selected profile would run, or why it may not.
//
// This is where the forced update is enforced, rather than only in the UI.
// Wails methods are callable from devtools and profiles.json is an ordinary
// file, so a disabled button is a presentation choice and not a rule.
func (g *GameService) launchPlan() (string, error) {
	profile := g.core.Profiles.Selected()

	if !profile.IsLatest() {
		if !g.core.Install.Installed(profile.Tag) {
			return "", fmt.Errorf("%q is pinned to %s, which is not installed yet",
				profile.Name, profile.Tag)
		}
		return g.core.Layout.VersionDir(profile.Tag), nil
	}

	// Latest runs the newest build on disk — unless a newer one is known to be
	// published, which is precisely what this profile promises not to do.
	builds := g.core.Install.List()
	if len(builds) == 0 {
		return "", errors.New("no game installed yet")
	}
	newest := builds[0].Tag

	// Gated on knowing, not on guessing. An unreachable account server leaves
	// this unknown, and an unknown release must not become a locked door.
	if latest, ok := g.core.knownLatest(); ok && latest.Tag != newest {
		// And only when that release has something this platform can install:
		// a build nobody here could run is not an update being refused.
		if _, err := install.SelectAsset(latest); err == nil {
			return "", fmt.Errorf("%s is out, and the Latest profile always runs the newest build. Update to play.",
				latest.Tag)
		}
	}
	return g.core.Layout.VersionDir(newest), nil
}
