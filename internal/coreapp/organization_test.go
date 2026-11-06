package coreapp

import (
	"strings"
	"testing"
)

func TestOrganizationName(t *testing.T) {
	for _, raw := range []string{"", " \t ", "Team\x00name", "Team\nname", "Team\u0085name", strings.Repeat("x", 81), strings.Repeat("组", 27), string([]byte{0xff})} {
		if _, err := normalizeOrganizationName(raw); err == nil {
			t.Fatalf("accepted %q", raw)
		}
	}
	for _, raw := range []string{"  Team  ", strings.Repeat("x", 80), strings.Repeat("组", 26)} {
		name, err := normalizeOrganizationName(raw)
		if err != nil || name != strings.TrimSpace(raw) {
			t.Fatalf("rejected %q: %v", raw, err)
		}
	}
}
