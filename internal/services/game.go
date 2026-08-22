package services

import (
	"context"
	"errors"
	"os"
	"strings"

	"github.com/gustaavik/wc-launcher/internal/gamesvc"
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

	opts := gamesvc.Options{
		DataDir:    g.core.Layout.Data,
		AuthURL:    g.core.Settings.ResolvedAuthURL(),
		VersionDir: versionDir,
		LogFilter:  g.core.Settings.LogFilter,
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

// versionDir is the install to launch: the dev override if set, otherwise the
// recorded one.
func (g *GameService) versionDir() (string, error) {
	if dev := strings.TrimSpace(os.Getenv(devGameDirVar)); dev != "" {
		if _, err := os.Stat(dev); err != nil {
			return "", errors.New(devGameDirVar + " is set but " + dev + " does not exist")
		}
		return dev, nil
	}

	state := g.core.Install.State()
	if state.Tag == "" || !g.core.Install.Installed(state.Tag) {
		return "", errors.New("no game installed yet")
	}
	return g.core.Layout.VersionDir(state.Tag), nil
}
