package gamesvc

import (
	"os"
	"path/filepath"
)

// Wyvencraft renders through Vulkan, which on macOS means MoltenVK translating
// to Metal. The loader finds a driver through VK_ICD_FILENAMES /
// VK_DRIVER_FILES, and finds MoltenVK's own dylib through DYLD_LIBRARY_PATH.
//
// The game's checkout sets these in .cargo/config.toml, which cargo applies to
// `cargo run`. That file is gitignored and applies to cargo, not to a binary
// started directly — so a downloaded build inherits none of it, and the
// launcher has to supply them or the loader reports no driver at all.
//
// The release tarball does not bundle MoltenVK today, hence the search.

// icdCandidates are checked in order; the first that exists wins.
//
// The bundled path comes first so that a future release which ships its own
// MoltenVK is preferred over whatever the machine happens to have.
var icdCandidates = []struct {
	icd string
	lib string
}{
	{"", ""}, // placeholder for the bundled copy, filled in by probeVulkan
	{"/opt/homebrew/etc/vulkan/icd.d/MoltenVK_icd.json", "/opt/homebrew/lib"},
	{"/opt/homebrew/share/vulkan/icd.d/MoltenVK_icd.json", "/opt/homebrew/lib"},
	{"/usr/local/etc/vulkan/icd.d/MoltenVK_icd.json", "/usr/local/lib"},
	{"/usr/local/share/vulkan/icd.d/MoltenVK_icd.json", "/usr/local/lib"},
}

// VulkanHint is what to tell a player when no driver could be found.
const VulkanHint = "brew install molten-vk vulkan-loader"

// probeVulkan finds a Vulkan driver and returns the variables that point at it.
//
// The second return value is false when none was found, which the caller turns
// into a refusal to launch. Starting the game anyway produces a crash with a
// message no player can act on.
func probeVulkan(versionDir string) (map[string]string, bool) {
	// Already configured for us — a developer running the launcher from a shell
	// that has the variables set. Honour it rather than second-guessing.
	if icd := os.Getenv("VK_ICD_FILENAMES"); icd != "" {
		if _, err := os.Stat(icd); err == nil {
			return nil, true
		}
	}

	candidates := make([][2]string, 0, len(icdCandidates))
	if versionDir != "" {
		bundled := filepath.Join(versionDir, "MoltenVK", "MoltenVK_icd.json")
		candidates = append(candidates, [2]string{bundled, filepath.Join(versionDir, "MoltenVK")})
	}
	for _, c := range icdCandidates[1:] {
		candidates = append(candidates, [2]string{c.icd, c.lib})
	}
	if sdk := os.Getenv("VULKAN_SDK"); sdk != "" {
		candidates = append(candidates, [2]string{
			filepath.Join(sdk, "share", "vulkan", "icd.d", "MoltenVK_icd.json"),
			filepath.Join(sdk, "lib"),
		})
	}

	for _, c := range candidates {
		icd, lib := c[0], c[1]
		if _, err := os.Stat(icd); err != nil {
			continue
		}
		env := map[string]string{
			// Both names: the loader renamed this variable, and which one it
			// reads depends on the loader's version.
			"VK_ICD_FILENAMES": icd,
			"VK_DRIVER_FILES":  icd,
		}
		if lib != "" {
			if existing := os.Getenv("DYLD_LIBRARY_PATH"); existing != "" {
				env["DYLD_LIBRARY_PATH"] = lib + ":" + existing
			} else {
				env["DYLD_LIBRARY_PATH"] = lib
			}
		}
		return env, true
	}
	return nil, false
}
