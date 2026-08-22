# Wyvencraft Launcher

Signs the player in, keeps the game up to date, and starts it.

The game itself has **no login screen** — this is what replaced it. The launcher
authenticates against [wcauthserver], writes the session into the game's
`profile.toml`, and starts the game pointed at a data directory that survives
updates.

```
                  ┌──────────────────────────────────────────┐
   wc-launcher ──▶│ wcauthserver                             │
                  │  POST /api/v1/auth/login    → session    │
                  │  GET  /api/v1/keys          → authkeys   │
                  │  GET  /api/v1/releases/…    → build info │──▶ GitHub
                  └──────────────────────────────────────────┘   (private repo)
        │
        ├─ writes  <data>/profile.toml     the session the game restores
        ├─ writes  <data>/authkeys.toml    so hosting works
        └─ spawns  <versions>/<tag>/wyvencraft
                     cwd = the version dir      (finds assets/)
                     WYVEN_DATA_DIR = <data>    (finds saves/)
```

## On disk

Everything lives under one root — `~/Library/Application Support/Wyvencraft`
on macOS, `%APPDATA%\Wyvencraft` on Windows, `~/.local/share/Wyvencraft`
otherwise:

```
launcher.json      launcher settings (account server, log filter)
installed.json     which build is installed
versions/<tag>/    an installed build: the wyvencraft binary plus assets/
data/              the game's WYVEN_DATA_DIR
  saves/  profile.toml  authkeys.toml  ops.toml
logs/              launcher.log, game.log
```

`versions/` and `data/` are separate on purpose: applying an update replaces a
whole version directory, so nothing that must survive one may live inside it.
The game agrees with this layout — its `src/paths.rs` resolves the same default,
so starting it by hand finds the same worlds.

## Running it

```sh
wails3 dev      # hot reload, both sides
wails3 task build     # -> bin/wc-launcher
wails3 task package   # -> bin/Wyvencraft Launcher.app  (macOS)
go test ./internal/...
```

`go-task` need not be installed separately; `wails3 task` runs the same targets.

### Against a local account server

Set the account server in Settings, or write it directly:

```sh
echo '{"authUrl":"http://localhost:8080"}' \
  > ~/Library/Application\ Support/Wyvencraft/launcher.json
```

The server needs `GITHUB_RELEASES_TOKEN` set for downloads to work; without it
`/healthz` reports `updates_enabled: false` and the launcher says so.

### Against a locally built game

Releases lag the game repo. To exercise the whole launch path against a build
from a checkout rather than a download:

```sh
cd ~/Developement/wyvencraft && cargo build --release
mkdir -p /tmp/wc-devgame && cp target/release/wyvencraft /tmp/wc-devgame/
cp -R assets /tmp/wc-devgame/

WCL_DEV_GAME_DIR=/tmp/wc-devgame ./bin/wc-launcher
```

### Integration tests

Skipped by default so `go test ./...` stays hermetic. They talk to a real
account server and download a real release:

```sh
WC_IT_AUTH_URL=http://localhost:8080 \
WC_IT_USERNAME=someone WC_IT_PASSWORD=... \
WCL_DEV_GAME_DIR=/tmp/wc-devgame \
  go test ./internal/services/ -v
```

## Layout

| Package | What it owns |
| --- | --- |
| `internal/paths` | The directory layout above. Must agree with the game's `src/paths.rs` |
| `internal/wcauth` | The account-server client: login, refresh, logout, keys, releases |
| `internal/profile` | `profile.toml` and `authkeys.toml` — the handoff to the game |
| `internal/install` | Asset selection, resumable download, checksum, unpack |
| `internal/gamesvc` | Child environment, Vulkan discovery, spawn, stderr streaming |
| `internal/markdown` | Release notes → HTML, with raw HTML dropped |
| `internal/services` | The three objects the frontend calls, and the session rules |

## Two rules worth not breaking

**The refresh token is stored once.** It lives in the game's `profile.toml` and
nowhere else. Rotation is destructive and single-use, so a second copy would
inevitably become a stale one, and presenting a spent token revokes every
session for the account.

**The launcher touches no token while the game runs.** The game refreshes the
same family; two concurrent refreshes read as reuse server-side and sign the
player out everywhere. `services.ErrGameRunning` enforces this, and the session
is re-read from disk once the game exits.

## Vulkan on macOS

The game renders through MoltenVK, and the release tarball does not bundle it.
The launcher looks for a driver (Homebrew, `/usr/local`, `$VULKAN_SDK`, or a
`MoltenVK/` directory inside the build) and sets `VK_ICD_FILENAMES`,
`VK_DRIVER_FILES` and `DYLD_LIBRARY_PATH`. If none is found it refuses to launch
and suggests `brew install molten-vk vulkan-loader`, which is more useful than a
crash inside the loader.

[wcauthserver]: https://github.com/gustaavik/wcauthserver
