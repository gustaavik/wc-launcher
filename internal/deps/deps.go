// Package deps installs the runtime libraries the game needs but does not ship.
//
// There is exactly one today: MoltenVK, the Vulkan-to-Metal driver every macOS
// player needs. The game's own .cargo/config.toml points cargo at a Homebrew
// copy, but that file is gitignored and applies to `cargo run`, not to a
// downloaded binary — so before this package existed a player was told to run
// `brew install molten-vk` before the Play button worked. Installing Homebrew
// is not a reasonable thing to ask of someone who bought a game.
//
// The driver is installed beside versions/ rather than inside one, because a
// game update replaces a whole version directory (see paths.Layout.MoltenVKDir).
package deps

// Version is the MoltenVK release the launcher installs.
//
// It names the directory the driver lands in, so bumping it installs alongside
// the old one rather than over it; the old directory is left for Prune-free
// manual cleanup, since it is a few megabytes and deleting a driver a running
// game may still have mapped is not worth the risk.
const Version = "1.4.2"

// apiVersion is the Vulkan version the generated ICD manifest advertises. It
// must match what this MoltenVK build actually implements.
const apiVersion = "1.4.0"

// The pinned asset. Variables rather than constants so a test can point them at
// a httptest server; nothing outside this package's tests writes them.
//
// This is our own repackaging of KhronosGroup/MoltenVK's MoltenVK-macos.tar:
// the universal dylib and its licence, and nothing else. The upstream archive
// is 60 MB of headers and a static library around a 10 MB dylib, and asking
// every player to download that is the opposite of the point.
var (
	assetURL    = "https://github.com/gustaavik/wc-launcher/releases/download/deps%2Fmoltenvk-" + Version + "/moltenvk-" + Version + "-macos-universal.tar.gz"
	assetSHA256 = "bf3fa76db4d01efede0f3d08d3f9e43497316d968d6b2c672c1df42c5fbe002f"
	assetSize   = int64(3300820)
)

// DylibName is the file the Vulkan client library dlopens. vulkano tries
// libvulkan.dylib, libvulkan.1.dylib and then this, so the driver is usable
// with no Vulkan loader installed at all.
const DylibName = "libMoltenVK.dylib"

// ManifestName is the ICD manifest a Vulkan *loader* reads, for the case where
// the machine has one. It is generated on install rather than shipped, so its
// library_path is always correct relative to wherever the driver landed.
const ManifestName = "MoltenVK_icd.json"

// markerName records what is installed, so Ensure can tell a complete install
// from a directory that happens to exist.
const markerName = ".moltenvk.json"

// phase is the single progress phase this package reports under. The download,
// the checksum and the unpack are one step as far as a player is concerned:
// "the launcher is getting the graphics driver ready".
const phase = "dependencies"
