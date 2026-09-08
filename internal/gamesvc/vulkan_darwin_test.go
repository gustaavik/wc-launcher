package gamesvc

import (
	"os"
	"path/filepath"
	"testing"
)

// isolate empties the system candidate list and clears the environment
// overrides, so a test measures the code and not whether the developer running
// it happens to have Homebrew's MoltenVK installed.
func isolate(t *testing.T) {
	t.Helper()

	real := systemCandidates
	systemCandidates = nil
	t.Cleanup(func() { systemCandidates = real })

	t.Setenv("VK_ICD_FILENAMES", "")
	t.Setenv("VK_DRIVER_FILES", "")
	t.Setenv("DYLD_LIBRARY_PATH", "")
	t.Setenv("VULKAN_SDK", "")
}

// driverAt fabricates the two files a probe looks for.
func driverAt(t *testing.T, dir string) string {
	t.Helper()

	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	icd := filepath.Join(dir, "MoltenVK_icd.json")
	if err := os.WriteFile(icd, []byte(`{"ICD":{"library_path":"./libMoltenVK.dylib"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	return icd
}

// A release that ships MoltenVK ships the one it was built and tested against,
// which is a better answer than whatever the launcher last pinned.
func TestTheBuildsOwnDriverBeatsTheLauncherManagedOne(t *testing.T) {
	isolate(t)

	root := t.TempDir()
	versionDir := filepath.Join(root, "versions", "v1.2.3")
	bundled := driverAt(t, filepath.Join(versionDir, "MoltenVK"))
	managed := filepath.Join(root, "runtime", "moltenvk", "1.4.2")
	driverAt(t, managed)

	env, ok := probeVulkan(versionDir, managed)
	if !ok {
		t.Fatal("no driver found, with two installed")
	}
	if env["VK_ICD_FILENAMES"] != bundled {
		t.Errorf("chose %s, want the build's own %s", env["VK_ICD_FILENAMES"], bundled)
	}
}

func TestTheLauncherManagedDriverIsUsedWhenTheBuildHasNone(t *testing.T) {
	isolate(t)

	root := t.TempDir()
	versionDir := filepath.Join(root, "versions", "v1.2.3")
	if err := os.MkdirAll(versionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	managed := filepath.Join(root, "runtime", "moltenvk", "1.4.2")
	icd := driverAt(t, managed)

	env, ok := probeVulkan(versionDir, managed)
	if !ok {
		t.Fatal("the launcher-managed driver was not found")
	}
	if env["VK_ICD_FILENAMES"] != icd || env["VK_DRIVER_FILES"] != icd {
		t.Errorf("pointed at %q / %q, want both at %s",
			env["VK_ICD_FILENAMES"], env["VK_DRIVER_FILES"], icd)
	}
	// vulkano dlopens libMoltenVK.dylib by bare name, so the directory holding
	// it has to be on DYLD_LIBRARY_PATH or no loader-free client finds it.
	if env["DYLD_LIBRARY_PATH"] != managed {
		t.Errorf("DYLD_LIBRARY_PATH is %q, want %s", env["DYLD_LIBRARY_PATH"], managed)
	}
}

// A developer whose shell already points at a driver gets that driver.
func TestAnAlreadyConfiguredEnvironmentIsLeftAlone(t *testing.T) {
	isolate(t)

	root := t.TempDir()
	managed := filepath.Join(root, "runtime", "moltenvk", "1.4.2")
	driverAt(t, managed)
	t.Setenv("VK_ICD_FILENAMES", driverAt(t, filepath.Join(root, "shell")))

	env, ok := probeVulkan("", managed)
	if !ok {
		t.Fatal("an explicitly configured driver was rejected")
	}
	if env != nil {
		t.Errorf("overrode the environment with %v, want nothing added", env)
	}
}

// Pointing at a driver that is not there is not a configuration to honour.
func TestAStaleEnvironmentOverrideFallsThrough(t *testing.T) {
	isolate(t)

	root := t.TempDir()
	managed := filepath.Join(root, "runtime", "moltenvk", "1.4.2")
	icd := driverAt(t, managed)
	t.Setenv("VK_ICD_FILENAMES", filepath.Join(root, "gone", "MoltenVK_icd.json"))

	env, ok := probeVulkan("", managed)
	if !ok {
		t.Fatal("no driver found, with one installed")
	}
	if env["VK_ICD_FILENAMES"] != icd {
		t.Errorf("chose %q, want %s", env["VK_ICD_FILENAMES"], icd)
	}
}

// The refusal this reports is what makes the launcher install one instead, so
// it has to be reported and not papered over.
func TestNothingInstalledIsReportedAsNothingFound(t *testing.T) {
	isolate(t)

	root := t.TempDir()
	if _, ok := probeVulkan(filepath.Join(root, "versions", "v1"), filepath.Join(root, "runtime")); ok {
		t.Error("a driver was found on a machine that has none")
	}
	if VulkanReady(filepath.Join(root, "versions", "v1"), filepath.Join(root, "runtime")) {
		t.Error("VulkanReady disagrees with probeVulkan")
	}
}

// An existing DYLD_LIBRARY_PATH belongs to whoever set it.
func TestAnExistingLibraryPathIsPrependedToRatherThanReplaced(t *testing.T) {
	isolate(t)

	root := t.TempDir()
	managed := filepath.Join(root, "runtime", "moltenvk", "1.4.2")
	driverAt(t, managed)
	t.Setenv("DYLD_LIBRARY_PATH", "/somewhere/else")

	env, ok := probeVulkan("", managed)
	if !ok {
		t.Fatal("no driver found")
	}
	if want := managed + ":/somewhere/else"; env["DYLD_LIBRARY_PATH"] != want {
		t.Errorf("DYLD_LIBRARY_PATH is %q, want %q", env["DYLD_LIBRARY_PATH"], want)
	}
}
