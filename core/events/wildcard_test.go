package events

import (
	"testing"
)

// Test cases for wildcard matching
func TestWildcardMatching(t *testing.T) {
	testCases := []struct {
		pattern string
		str     string
		matches bool
	}{
		{"http.*", "http.get", true},
		{"http.*", "http.post", true},
		{"http.*", "ftp.get", false},
		{"*.txt", "report.txt", true},
		{"*.txt", "image.png", false},
		{"*", "anything", true},
		{"http.controller.*", "http.controller.users", true},
		{"http.controller.*", "http.service.users", false},
		{"http.*.action", "http.controller.action", true},
		{"http.*.action", "http.service.event", false},
		{"http.*", "http.controller.users", true},
		{"http.*.*", "http.controller.users", true},
		{"http.*.*", "ftp.service.events", false},
		{"*.*", "a.b", true},
		{"*.*", "a.b.c", true}, // extended ending
		{"*.*.*", "a.b.c", true},
		{"*.*.*", "a.b", false},
		{"http.controller.users.*", "http.controller.users.list", true},
		{"http.controller.users.*", "http.controller.products.list", false},
		{"*.controller.users.*", "http.controller.users.list", true},
		{"*.controller.users.*", "api.service.products.get", false},
	}

	for _, tc := range testCases {
		w := newWildcard(tc.pattern)
		result := w.match(tc.str)
		if result != tc.matches {
			t.Errorf("Pattern: %s, String: %s, Expected: %t, Got: %t", tc.pattern, tc.str, tc.matches, result)
		}
	}
}
