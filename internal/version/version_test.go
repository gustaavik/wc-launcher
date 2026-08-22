package version

import (
	"strings"
	"testing"
)

func TestIsDevOnlyForAnUnstampedBuild(t *testing.T) {
	original := Current
	t.Cleanup(func() { Current = original })

	for _, tc := range []struct {
		current string
		wantDev bool
	}{
		{"dev", true},
		{"", true},
		{"v0.1.0", false},
		{"v1.2.3-rc.1", false},
	} {
		Current = tc.current
		if got := IsDev(); got != tc.wantDev {
			t.Errorf("Current = %q: IsDev() = %v, want %v", tc.current, got, tc.wantDev)
		}
	}
}

// A stamped build compares its tag against GitHub's. A trailing newline from
// the release workflow would make every comparison report an update.
func TestCurrentHasNoSurroundingWhitespace(t *testing.T) {
	if Current != strings.TrimSpace(Current) {
		t.Errorf("Current = %q, want it trimmed", Current)
	}
}
