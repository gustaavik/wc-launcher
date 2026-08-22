// Command wc-launcher is the Wyvencraft launcher.
//
// It signs the player in, keeps the game up to date, and starts it. The game
// itself has no login screen: this hands it a session by writing the
// profile.toml in its data directory, and points it at that directory with
// WYVEN_DATA_DIR so saves survive an update.
package main

import (
	"embed"
	"log"
	"log/slog"
	"os"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/gustaavik/wc-launcher/internal/gamesvc"
	"github.com/gustaavik/wc-launcher/internal/install"
	"github.com/gustaavik/wc-launcher/internal/paths"
	"github.com/gustaavik/wc-launcher/internal/services"
)

//go:embed all:frontend/dist
var assets embed.FS

func init() {
	// Registering the payload types here is what gives the frontend typed
	// bindings for each event.
	application.RegisterEvent[*services.AccountView]("auth:changed")
	application.RegisterEvent[install.Progress]("update:progress")
	application.RegisterEvent[gamesvc.Status]("game:state")
	application.RegisterEvent[string]("game:log")
}

func main() {
	layout, err := paths.New()
	if err != nil {
		log.Fatalf("could not prepare the Wyvencraft directory: %v", err)
	}

	logFile, err := os.OpenFile(layout.Root+"/logs/launcher.log", os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err == nil {
		defer logFile.Close()
		slog.SetDefault(slog.New(slog.NewTextHandler(logFile, nil)))
	}
	slog.Info("launcher starting", "root", layout.Root)

	// The emitter is set once the app exists, so Core is built with none and
	// given one below. Events fired before then are dropped, which is fine:
	// nothing is listening yet either.
	core := services.NewCore(layout, nil)

	app := application.New(application.Options{
		Name:        "Wyvencraft Launcher",
		Description: "Sign in, keep Wyvencraft up to date, and play",
		Services: []application.Service{
			application.NewService(services.NewAuthService(core)),
			application.NewService(services.NewUpdateService(core)),
			application.NewService(services.NewGameService(core)),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})
	core.Emitter = appEmitter{app}

	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:  "Wyvencraft",
		Width:  980,
		Height: 660,
		// Below this the changelog and the version panel stop fitting side by
		// side, and the layout is not worth designing a third time.
		MinWidth:  820,
		MinHeight: 560,
		Mac: application.MacWindow{
			InvisibleTitleBarHeight: 44,
			Backdrop:                application.MacBackdropTranslucent,
			TitleBar:                application.MacTitleBarHiddenInset,
		},
		BackgroundColour: application.NewRGB(11, 14, 20),
		URL:              "/",
	})

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}

// appEmitter adapts the Wails application to services.Emitter, so nothing below
// main has to import Wails.
type appEmitter struct{ app *application.App }

func (e appEmitter) Emit(name string, data any) { e.app.Event.Emit(name, data) }
