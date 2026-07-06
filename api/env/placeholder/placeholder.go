// SPDX-License-Identifier: MPL-2.0

// Package placeholder parses the ${...} variable-reference grammar used in
// registry entry string values. It is a pure string parser with no registry
// dependencies, shared by the environment resolver and the topology dependency
// resolver.
package placeholder

import (
	"regexp"
	"strings"
)

// Grammar recognized inside entry string values:
//
//	${env:NAME}          ${env:NAME|default}
//	${NAME|default}      (NAME must be upper-snake; the default pipe is required)
//	$${                  literal ${
//
// The env: form allows registry-id style names (dots and colons); the shorthand
// only matches upper-snake identifiers and requires an explicit |default so that
// bare ${VAR} spans in embedded shell or template source are never treated as
// references. Anything else inside ${...} is left as-is.
var (
	envNameRe   = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.:-]*$`)
	shorthandRe = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)
)

// Segment is a piece of a parsed string: either literal text or a placeholder.
type Segment struct {
	Literal    string
	Name       string
	Default    string
	HasDefault bool
	IsRef      bool
}

// parseBody interprets the text between ${ and } and reports whether it is a
// recognized placeholder along with its name and optional default.
func parseBody(inner string) (name, def string, hasDefault, ok bool) {
	body := inner
	if idx := strings.IndexByte(body, '|'); idx >= 0 {
		def = body[idx+1:]
		hasDefault = true
		body = body[:idx]
	}

	if rest, isEnv := strings.CutPrefix(body, "env:"); isEnv {
		if envNameRe.MatchString(rest) {
			return rest, def, hasDefault, true
		}
		return "", "", false, false
	}

	if hasDefault && shorthandRe.MatchString(body) {
		return body, def, hasDefault, true
	}
	return "", "", false, false
}

// Parse splits a string into literal and placeholder segments, applying the
// $${ escape. Unrecognized ${...} spans are preserved as literal text.
func Parse(s string) []Segment {
	var segs []Segment
	var lit strings.Builder
	flush := func() {
		if lit.Len() > 0 {
			segs = append(segs, Segment{Literal: lit.String()})
			lit.Reset()
		}
	}

	for i := 0; i < len(s); {
		if s[i] == '$' {
			if i+2 < len(s) && s[i+1] == '$' && s[i+2] == '{' {
				lit.WriteString("${")
				i += 3
				continue
			}
			if i+1 < len(s) && s[i+1] == '{' {
				if closing := strings.IndexByte(s[i+2:], '}'); closing >= 0 {
					inner := s[i+2 : i+2+closing]
					if name, def, hasDefault, ok := parseBody(inner); ok {
						flush()
						segs = append(segs, Segment{Name: name, Default: def, HasDefault: hasDefault, IsRef: true})
						i += 2 + closing + 1
						continue
					}
				}
				lit.WriteByte('$')
				i++
				continue
			}
		}
		lit.WriteByte(s[i])
		i++
	}
	flush()
	return segs
}

// ExtractNames returns the variable names referenced by every recognized
// placeholder in s, in order of first appearance without duplicates. A string
// without "${" returns nil at near-zero cost.
func ExtractNames(s string) []string {
	if !strings.Contains(s, "${") {
		return nil
	}

	var names []string
	seen := make(map[string]struct{})
	for _, seg := range Parse(s) {
		if !seg.IsRef {
			continue
		}
		if _, dup := seen[seg.Name]; dup {
			continue
		}
		seen[seg.Name] = struct{}{}
		names = append(names, seg.Name)
	}
	return names
}
