package wildcard

import (
	"strings"
)

// Wildcard represents a wildcard pattern that can match strings.
type Wildcard struct {
	segments []string
}

// NewWildcard splits the pattern into segments.
func NewWildcard(pattern string) *Wildcard {
	if pattern == "" {
		return &Wildcard{segments: []string{}}
	}
	segments := strings.Split(pattern, ".")
	return &Wildcard{segments: segments}
}

// Match checks if the input string matches the Wildcard pattern.
func (w *Wildcard) Match(str string) bool {
	strSegments := strings.Split(str, ".")
	return matchSegments(w.segments, strSegments)
}

// matchSegments recursively matches pattern segments with string segments.
func matchSegments(pattern, str []string) bool {
	pLen, sLen := len(pattern), len(str)
	p, s := 0, 0

	for p < pLen && s < sLen {
		switch pattern[p] {
		case "**":
			// If '**' is the last pattern, it matches the rest of the string
			if p == pLen-1 {
				return true
			}
			// Try to match the rest of the pattern with every possible substring
			for i := s; i <= sLen; i++ {
				if matchSegments(pattern[p+1:], str[i:]) {
					return true
				}
			}
			return false
		case "*":
			// '*' matches exactly one segment
			p++
			s++
		default:
			// Handle exact match or alternations
			if !matchSegment(pattern[p], str[s]) {
				return false
			}
			p++
			s++
		}
	}

	// Handle trailing '**' in the pattern
	for p < pLen && pattern[p] == "**" {
		p++
	}

	return p == pLen && s == sLen
}

// matchSegment handles exact matches and alternations like (a|b).
func matchSegment(patternSegment, strSegment string) bool {
	if strings.HasPrefix(patternSegment, "(") && strings.HasSuffix(patternSegment, ")") {
		options := strings.Split(patternSegment[1:len(patternSegment)-1], "|")
		for _, opt := range options {
			if opt == strSegment {
				return true
			}
		}
		return false
	}
	return patternSegment == strSegment
}
