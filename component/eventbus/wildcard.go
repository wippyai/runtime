package eventsbus

import (
	"fmt"
	"strings"
)

type wildcard struct {
	prefix string
	suffix string
}

// newWildcard creates a new wildcard pattern
// accepts patterns like:
// foo.*
// *.bar
// foo.bar
// *.*
func newWildcard(pattern string) *wildcard {
	// Normalize
	origin := strings.ToLower(pattern)
	dot := strings.IndexByte(origin, '.')

	/*
		foo.*
		*
		*.bar
	*/
	if dot == -1 {
		// should never happen
		panic(fmt.Sprintf("invalid pattern: %s", pattern))
	}

	// pref: config. (for example)
	// suff: *
	return &wildcard{origin[0:dot], origin[dot+1:]}
}

func (w wildcard) match(s string) bool {
	var pref bool
	var suff bool
	wc := newWildcard(strings.ToLower(s))
	if w.prefix == "*" || wc.prefix == "*" {
		pref = true
	} else {
		pref = w.prefix == wc.prefix
	}

	if w.suffix == "*" || wc.suffix == "*" {
		suff = true
	} else {
		suff = w.suffix == wc.suffix
	}

	return pref && suff
}
