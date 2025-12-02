package wildcard

import (
	"testing"
)

func TestWildcard_FastPaths(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		str     string
		want    bool
	}{
		// Star pattern fast path
		{"star matches single segment", "*", "anything", true},
		{"star no match multi segment", "*", "a.b", false},
		{"star no match empty", "*", "", false},

		// Exact match fast path (no wildcards)
		{"exact match", "system.event", "system.event", true},
		{"exact no match", "system.event", "system.other", false},
		{"exact no match partial", "system.event", "system", false},
		{"exact no match longer", "system.event", "system.event.extra", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := NewWildcard(tt.pattern)
			if got := w.Match(tt.str); got != tt.want {
				t.Errorf("Match() = %v, want %v", got, tt.want)
			}
		})
	}
}

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

// Benchmarks

func BenchmarkMatch_ExactMatch(b *testing.B) {
	w := NewWildcard("system.events.user.created")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w.Match("system.events.user.created")
	}
}

func BenchmarkMatch_Star(b *testing.B) {
	w := NewWildcard("*")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w.Match("anything")
	}
}

func BenchmarkMatch_SingleWildcard(b *testing.B) {
	w := NewWildcard("system.*.created")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w.Match("system.events.created")
	}
}

func BenchmarkMatch_DoubleWildcard(b *testing.B) {
	w := NewWildcard("system.**")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w.Match("system.events.user.created")
	}
}

func BenchmarkMatch_Alternation(b *testing.B) {
	w := NewWildcard("(a|b|c).event")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		w.Match("b.event")
	}
}

func BenchmarkNewWildcard(b *testing.B) {
	for i := 0; i < b.N; i++ {
		NewWildcard("system.events.*.created")
	}
}
