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
		{"web_server.*", "web_server.get", true},
		{"web_server.*", "web_server.post", true},
		{"web_server.*", "ftp.get", false},
		{"*.txt", "report.txt", true},
		{"*.txt", "image.png", false},
		{"*", "anything", true},
		{"web_server.controller.*", "web_server.controller.users", true},
		{"web_server.controller.*", "web_server.service.users", false},
		{"web_server.*.action", "web_server.controller.action", true},
		{"web_server.*.action", "web_server.service.event", false},
		{"web_server.*", "web_server.controller.users", true},
		{"web_server.*.*", "web_server.controller.users", true},
		{"web_server.*.*", "ftp.service.events", false},
		{"*.*", "a.b", true},
		{"*.*", "a.b.c", true}, // extended ending
		{"*.*.*", "a.b.c", true},
		{"*.*.*", "a.b", false},
		{"web_server.controller.users.*", "web_server.controller.users.list", true},
		{"web_server.controller.users.*", "web_server.controller.products.list", false},
		{"*.controller.users.*", "web_server.controller.users.list", true},
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
