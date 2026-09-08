//go:build !darwin

package gamesvc

// VulkanHint is what to tell a player when no driver could be found.
//
// On Windows and Linux the Vulkan loader is installed with the GPU driver and
// found without help, so there is nothing for the launcher to set — and no
// useful advice beyond updating the driver.
const VulkanHint = "install or update your graphics driver"

// VulkanReady reports whether a driver is already available. Always true here,
// for the same reason probeVulkan always succeeds.
func VulkanReady(versionDir, moltenVKDir string) bool { return true }

// probeVulkan contributes no environment on these platforms.
//
// It always reports success: unlike macOS, there is no reliable file to look
// for, and refusing to launch on a false negative would be worse than letting
// the game report the problem itself.
func probeVulkan(versionDir, moltenVKDir string) (map[string]string, bool) {
	return nil, true
}
