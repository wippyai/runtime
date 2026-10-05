// SPDX-License-Identifier: MPL-2.0

package wildcard

import (
	"math/rand"
	"strings"
	"testing"
)

var matchedSink bool

func TestMatchSegmentDoesNotAllocate(t *testing.T) {
	allocs := testing.AllocsPerRun(1000, func() {
		matchedSink = matchSegment("(register|unregister|invalidate)", "invalidate")
	})
	if allocs != 0 {
		t.Fatalf("matching a preexisting alternation allocated %.0f objects", allocs)
	}
}

func referenceMatchSegment(pattern, value string) bool {
	if strings.HasPrefix(pattern, "(") && strings.HasSuffix(pattern, ")") {
		for _, option := range strings.Split(pattern[1:len(pattern)-1], "|") {
			if option == value {
				return true
			}
		}
		return false
	}
	return pattern == value
}

func TestMatchSegmentAlternationParity(t *testing.T) {
	patterns := []string{"", "a", "(", ")", "()", "(|)", "(a|)", "(|a)", "(a||b)", "(a|a)", "( a |b)", "((a)|b)", "(é|日本語|\x00)", "a|b", "(*|**)"}
	values := []string{"", "a", "b", " a ", "(a)", "é", "日本語", "\x00", "*", "**"}
	check := func(pattern, value string) {
		t.Helper()
		if got, want := matchSegment(pattern, value), referenceMatchSegment(pattern, value); got != want {
			t.Fatalf("pattern=%q value=%q: got %v want %v", pattern, value, got, want)
		}
	}
	for _, pattern := range patterns {
		for _, value := range values {
			check(pattern, value)
		}
	}
	rng := rand.New(rand.NewSource(42))
	alphabet := []byte("abc|().* \x00")
	text := func() string {
		data := make([]byte, rng.Intn(48))
		for i := range data {
			data[i] = alphabet[rng.Intn(len(alphabet))]
		}
		return string(data)
	}
	for range 100000 {
		pattern, value := text(), text()
		if rng.Intn(2) == 0 {
			pattern = "(" + pattern + ")"
		}
		if rng.Intn(3) == 0 && strings.HasPrefix(pattern, "(") && strings.HasSuffix(pattern, ")") {
			options := strings.Split(pattern[1:len(pattern)-1], "|")
			value = options[rng.Intn(len(options))]
		}
		check(pattern, value)
	}
}
