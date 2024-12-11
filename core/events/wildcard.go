package events

import (
	"strings"
)

// wildcard represents a parsed wildcard pattern.
type wildcard struct {
	segments []string
}

// newWildcard creates a new wildcard from a pattern string.
func newWildcard(pattern string) *wildcard {
	return &wildcard{
		segments: strings.Split(pattern, "."),
	}
}

// match checks if a string matches the wildcard pattern.
func (w *wildcard) match(str string) bool {
	strSegments := strings.Split(str, ".")

	for i := 0; i < len(w.segments); i++ {
		if i >= len(strSegments) {
			return false
		}

		if w.segments[i] == "*" {
			continue
		}

		if w.segments[i] != strSegments[i] {
			return false
		}
	}

	return true
}
