package runrecord

import (
	"strings"
	"testing"
)

func TestSessionDeclarationValidation(t *testing.T) {
	for _, id := range []string{"", "night-2026_11.09", "a", strings.Repeat("x", 64)} {
		if err := ValidateSessionID(id); err != nil {
			t.Fatalf("valid declaration rejected: %q %v", id, err)
		}
	}
	for _, id := range []string{"has space", "../escape", "_leading", "ümlaut", strings.Repeat("x", 65), "line\nnext"} {
		if err := ValidateSessionID(id); err == nil {
			t.Fatalf("invalid declaration accepted: %q", id)
		}
	}
}
