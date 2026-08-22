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
launcher-update/   a downloaded launcher, until it replaces the running one
```

`versions/` and `data/` are separate on purpose: applying an update replaces a
whole version directory, so nothing that must survive one may live inside it.
The game agrees with this layout — its `src/paths.rs` resolves the same default,
so starting it by hand finds the same worlds.

## Running it

```sh
wails3 dev                            # hot reload, both sides
wails3 task build                     # -> bin/Wyvencraft
wails3 task package                   # -> bin/Wyvencraft.app  (host arch)
wails3 task darwin:package:universal   # -> bin/Wyvencraft.app  (arm64 + amd64)
go test -race ./internal/...
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
| `internal/selfupdate` | The launcher's own update: GitHub releases, staging, the swap |
| `internal/version` | Which build this is, stamped in by the release workflow |
| `internal/gamesvc` | Child environment, Vulkan discovery, spawn, stderr streaming |
| `internal/markdown` | Release notes → HTML, with raw HTML dropped |
| `internal/services` | The three objects the frontend calls, and the session rules |

## Updating the launcher itself

The game is updated through wcauthserver, which brokers downloads from the
private game repository. The launcher is not: this repository is public, so the
launcher reads its own releases straight from the GitHub API, with no token and
no account. That matters — a launcher too old to sign in is exactly the one that
has to be able to replace itself.

It checks once at startup and offers a strip on the home screen. **Download**
fetches the release asset, verifies its SHA-256, unpacks it into
`launcher-update/`, and checks that the bundle is signed by team `S6EF64ZEMD`
before going anywhere near the installed copy — TLS and the checksum prove the
bytes came from GitHub intact, and the signature proves GitHub was serving ours.

**Restart to update** hands over to the downloaded build. A process cannot
replace itself, so the new launcher does it: it starts with `--apply-update
<target> <pid>`, waits for this one to quit, copies itself into place with
`ditto`, and reopens. The copy lands beside the target first, so a failure never
leaves the Applications folder without a launcher in it.

Neither step runs while Wyvencraft does. The rules below are why.

## Releasing

Publish a GitHub release and `.github/workflows/release.yml` does the rest: it
builds a universal `.app`, signs it with the Developer ID certificate, notarizes
it, staples the ticket, and attaches
`wc-launcher-<tag>-macos-universal.zip` plus its `.sha256` to the release.

```sh
gh release create v0.2.0 --generate-notes
```

The tag is what the launcher compares itself against, and the workflow stamps it
into `internal/version/VERSION` before building. A local build says `dev` and
never offers to update itself.

Six repository secrets, all macOS signing:

| Secret | What |
| --- | --- |
| `MACOS_CERT_P12` | base64 of the exported Developer ID Application `.p12` |
| `MACOS_CERT_PASSWORD` | its export password |
| `MACOS_SIGN_IDENTITY` | `Developer ID Application: … (S6EF64ZEMD)` |
| `APPLE_API_KEY_P8` | base64 of the App Store Connect `.p8` |
| `APPLE_API_KEY_ID` | that key's id |
| `APPLE_API_ISSUER_ID` | the issuer id |

The workflow runs only on `release: published` and `workflow_dispatch`, both of
which execute in this repository's own context. **Do not add a `pull_request`
trigger**: this repository is public, and that would hand the signing key to
anyone who opens a fork PR.

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
