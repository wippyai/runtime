package wildcard

import (
	"testing"
)

func TestWildcardMatching(t *testing.T) {
	testCases := []struct {
		pattern string
		str     string
		matches bool
	}{
		// Basic Wildcard tests
		{"a.*", "a.b", true},
		{"a.*", "a.c", true},
		{"a.*", "a.b.c", false}, // '*' matches exactly one segment

		// Wildcard matches multiple segments with '**'
		{"a.**", "a.b", true},
		{"a.**", "a.b.c", true},
		{"a.**", "a.b.c.d", true},
		{"a.**", "a", true}, // '**' can match zero segments

		// Multiple wildcards
		{"*.*.*", "a.b.c", true},
		{"*.*.*", "a.b", false},
		{"*.*.*", "a.b.c.d", false},

		// Wildcard at start
		{"**.state.*", "a.state.x", true},
		{"**.state.*", "b.state.y.z", false}, // '**' consumes too much
		{"**.state.*", "c.state.x", true},

		// Exact matches without wildcards
		{"a.b.c", "a.b.c", true},
		{"a.b.c", "a.b", false},
		{"a.b.c", "a.b.c.d", false},

		// Patterns with alternations
		{"(a|b).(b|y).c", "a.b.c", true},
		{"(a|b).(b|y).c", "b.y.c", true},
		{"(a|b).(b|y).c", "a.y.c", true},
		{"(a|b).(b|y).c", "c.b.c", false},

		// Mixed wildcards and literals
		{"a.*.c", "a.b.c", true},
		{"a.*.c", "a.x.y", false},
		{"a.*.c", "a.b.d.c", false},
		{"a.b.*.c", "a.b.x.c", true},
		{"a.b.*.c", "a.b.x.y.c", false},
		{"a.b.*.c", "a.x.y.c", false},

		// Edge Cases
		{"*", "anything", true},
		{"a", "a", true},
		{"a", "ab", false},
		{"", "", false},
		{"a", "", false},
		{"", "a", false},

		// More complex scenarios
		{"(a|b).b.state.*", "a.b.state.x", true},
		{"(a|b).b.state.*", "b.b.state.x", true},
		{"(a|b).state.*", "a.d.state.x", false},
		{"(a|b).(a|b|c).state.*", "a.c.state.x", true},

		// Mixed wildcards
		{"*.state.*", "a.state.x", true},
		{"*.state.*", "b.state.y.z", false}, // '*' matches exactly one segment
		{"*.state.*", "b.event.y.z", false},
		{"*.*.state.*", "a.b.state.x", true},
		{"*.*.state.*", "a.b.c.x", false},
		{"*.*.state.*", "a.b.state", false},
		{"a.*.*.d", "a.b.c.d", true},
		{"a.*.*.d", "a.b.x.y", false},
	}

	for _, tc := range testCases {
		w := NewWildcard(tc.pattern)
		result := w.Match(tc.str)
		if result != tc.matches {
			t.Errorf("Pattern: %s, String: %s, Expected: %t, Got: %t",
				tc.pattern, tc.str, tc.matches, result)
		}
	}
}
