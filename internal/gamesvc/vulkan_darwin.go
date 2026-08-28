package gamesvc

import (
	"os"
	"path/filepath"
)

// Wyvencraft renders through Vulkan, which on macOS means MoltenVK translating
// to Metal. A Vulkan loader finds a driver through VK_ICD_FILENAMES /
// VK_DRIVER_FILES; the client library finds MoltenVK's own dylib through
// DYLD_LIBRARY_PATH, and needs no loader at all — vulkano dlopens
// libvulkan.dylib, libvulkan.1.dylib and then libMoltenVK.dylib in turn. Both
// are set, so either route lands on the same driver.
//
// The game's checkout sets these in .cargo/config.toml, which cargo applies to
// `cargo run`. That file is gitignored and applies to cargo, not to a binary
// started directly — so a downloaded build inherits none of it, and the
// launcher has to supply them or the loader reports no driver at all.
//
// Nothing here installs anything: internal/deps does that, and hands the
// directory it installed into probeVulkan as moltenVKDir.

// systemCandidates are the drivers somebody else installed. Checked after the
// two the launcher controls, so a copy we know the version of always wins.
//
// A variable so a test can empty it: on a developer's Mac the Homebrew entry
// exists, and a probe that finds it would pass whatever the code did.
var systemCandidates = [][2]string{
	{"/opt/homebrew/etc/vulkan/icd.d/MoltenVK_icd.json", "/opt/homebrew/lib"},
	{"/opt/homebrew/share/vulkan/icd.d/MoltenVK_icd.json", "/opt/homebrew/lib"},
	{"/usr/local/etc/vulkan/icd.d/MoltenVK_icd.json", "/usr/local/lib"},
	{"/usr/local/share/vulkan/icd.d/MoltenVK_icd.json", "/usr/local/lib"},
}

// VulkanHint is what to tell a player when no driver could be found. It reads
// as the second half of ErrNoVulkan.
//
// The launcher installs one itself, so reaching this means that failed too.
// Homebrew is the way out by hand, not the expected path.
const VulkanHint = "and installing one did not succeed. Check your connection and try again, or install it by hand with: brew install molten-vk"

// VulkanReady reports whether a driver is already available, without starting
// anything. It is how the caller decides to install one first.
func VulkanReady(versionDir, moltenVKDir string) bool {
	_, ok := probeVulkan(versionDir, moltenVKDir)
	return ok
}

// probeVulkan finds a Vulkan driver and returns the variables that point at it.
//
// The second return value is false when none was found, which the caller turns
// into a refusal to launch. Starting the game anyway produces a crash with a
// message no player can act on.
func probeVulkan(versionDir, moltenVKDir string) (map[string]string, bool) {
	// Already configured for us — a developer running the launcher from a shell
	// that has the variables set. Honour it rather than second-guessing.
	if icd := os.Getenv("VK_ICD_FILENAMES"); icd != "" {
		if _, err := os.Stat(icd); err == nil {
			return nil, true
		}
	}

	candidates := make([][2]string, 0, len(systemCandidates)+3)
	// The build's own copy first: a release that ships MoltenVK ships the one
	// it was tested against, which beats anything else on the machine.
	if versionDir != "" {
		dir := filepath.Join(versionDir, "MoltenVK")
		candidates = append(candidates, [2]string{filepath.Join(dir, "MoltenVK_icd.json"), dir})
	}
	// Then the one the launcher installed and knows the version of.
	if moltenVKDir != "" {
		candidates = append(candidates, [2]string{filepath.Join(moltenVKDir, "MoltenVK_icd.json"), moltenVKDir})
	}
	candidates = append(candidates, systemCandidates...)
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
