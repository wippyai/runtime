package events

import (
	"strings"
)

type wildcard struct {
	segments []string
}

// newWildcard splits the pattern into segments.
func newWildcard(pattern string) *wildcard {
	if pattern == "" {
		return &wildcard{segments: []string{}}
	}
	segments := strings.Split(pattern, ".")
	return &wildcard{segments: segments}
}

// Match checks if the input string matches the wildcard pattern.
func (w *wildcard) Match(str string) bool {
	strSegments := strings.Split(str, ".")
	return matchSegments(w.segments, strSegments)
}

// matchSegments recursively matches pattern segments with string segments.
func matchSegments(pattern, str []string) bool {
	pLen, sLen := len(pattern), len(str)
	p, s := 0, 0

	for p < pLen && s < sLen {
		if pattern[p] == "**" {
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
		} else if pattern[p] == "*" {
			// '*' matches exactly one segment
			p++
			s++
		} else {
			// Handle exact match or alternations
			if !matchSegment(pattern[p], str[s]) {
				return false
			}
			p++
			s++
		}
	}

	// Handle trailing '**' in pattern
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
