package runrecord

import (
	"fmt"
	"regexp"
)

var sessionPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// ValidateSessionID permits unknown legacy provenance (empty), never inventing
// a session from a run ID, process, clock or file modification time.
func ValidateSessionID(id string) error {
	if id != "" && !sessionPattern.MatchString(id) {
		return fmt.Errorf("session-id must be 1-64 ASCII letters/digits/._-, starting with a letter or digit; empty means unknown")
	}
	return nil
}
