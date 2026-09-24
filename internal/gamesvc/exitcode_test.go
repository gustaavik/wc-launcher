package gamesvc

import (
	"strings"
	"testing"
)

func TestExitMessageExplainsTheWindowsCodesAPlayerCanHit(t *testing.T) {
	for _, tc := range []struct {
		name string
		code int
		want []string
	}{
		// Go reports an NTSTATUS as the unsigned value, a large positive int.
		{"dll not found", 0xC0000135, []string{"missing", "reinstall"}},
		{"entry point not found", 0xC0000139, []string{"missing", "reinstall"}},
		{"bad image", 0xC000007B, []string{"reinstall"}},
		{"access violation", 0xC0000005, []string{"crashed", "log"}},
		// The game's EXIT_NO_VULKAN: no GPU it can run on.
		{"no vulkan", 3, []string{"vulkan", "graphics driver", "virtual machines"}},
		{"unknown", 7, []string{"code 7", "log"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := exitMessage(tc.code)
			for _, want := range tc.want {
				if !strings.Contains(strings.ToLower(got), strings.ToLower(want)) {
					t.Errorf("exitMessage(%#x) = %q, want it to mention %q", tc.code, got, want)
				}
			}
		})
	}
}

// Whatever the explanation, the raw code stays in the message: it is what a
// bug report needs, and hex is how Windows documents it.
func TestExitMessageKeepsTheCodeInHex(t *testing.T) {
	for _, code := range []int{0xC0000135, 0xC0000005, 0xC0001234} {
		if got := exitMessage(code); !strings.Contains(got, "0xC") {
			t.Errorf("exitMessage(%d) = %q, want the hex code in it", code, got)
		}
	}
}

func TestExitMessageForACleanExit(t *testing.T) {
	if got := exitMessage(0); got != "Wyvencraft closed." {
		t.Errorf("exitMessage(0) = %q", got)
	}
}
