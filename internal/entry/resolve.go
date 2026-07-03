// SPDX-License-Identifier: MPL-2.0

package entry

import (
	"context"
	"errors"
	"regexp"
	"strconv"
	"strings"

	"github.com/wippyai/runtime/api/env"
	"gopkg.in/yaml.v3"
)

// Placeholder grammar recognized inside entry string values:
//
//	${env:NAME}          ${env:NAME|default}
//	${NAME}              ${NAME|default}     (NAME must be upper-snake)
//	$${                  literal ${
//
// The env: form allows registry-id style names (dots and colons); the shorthand
// only matches upper-snake identifiers. Anything else inside ${...} is left as-is.
var (
	envNameRe   = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.:-]*$`)
	shorthandRe = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)
)

// segment is a piece of a parsed string: either literal text or a placeholder.
type segment struct {
	literal    string
	name       string
	def        string
	hasDefault bool
	isRef      bool
}

// parsePlaceholderBody interprets the text between ${ and } and reports whether
// it is a recognized placeholder along with its name and optional default.
func parsePlaceholderBody(inner string) (name, def string, hasDefault, ok bool) {
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

	if shorthandRe.MatchString(body) {
		return body, def, hasDefault, true
	}
	return "", "", false, false
}

// parseSegments splits a string into literal and placeholder segments, applying
// the $${ escape. Unrecognized ${...} spans are preserved as literal text.
func parseSegments(s string) []segment {
	var segs []segment
	var lit strings.Builder
	flush := func() {
		if lit.Len() > 0 {
			segs = append(segs, segment{literal: lit.String()})
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
					if name, def, hasDefault, ok := parsePlaceholderBody(inner); ok {
						flush()
						segs = append(segs, segment{name: name, def: def, hasDefault: hasDefault, isRef: true})
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

// ExtractPlaceholderNames returns the variable names referenced by every
// recognized placeholder in s, in order of first appearance without duplicates.
func ExtractPlaceholderNames(s string) []string {
	if !strings.Contains(s, "${") {
		return nil
	}

	var names []string
	seen := make(map[string]struct{})
	for _, seg := range parseSegments(s) {
		if !seg.isRef {
			continue
		}
		if _, dup := seen[seg.name]; dup {
			continue
		}
		seen[seg.name] = struct{}{}
		names = append(names, seg.name)
	}
	return names
}

// ResolveDataPlaceholders returns a copy of data with placeholders resolved
// against the environment registry in ctx. When no placeholder is present the
// input map is returned unchanged with no allocation. The input map is never
// mutated: copies are made only along paths that change.
func ResolveDataPlaceholders(ctx context.Context, data map[string]any) (map[string]any, error) {
	out, _, err := resolveDataMap(ctx, "", data)
	return out, err
}

// resolveDataMap resolves a map copy-on-write, reporting whether anything changed.
func resolveDataMap(ctx context.Context, path string, m map[string]any) (map[string]any, bool, error) {
	if m == nil {
		return nil, false, nil
	}

	var out map[string]any
	for k, v := range m {
		nv, changed, err := resolveValue(ctx, joinPath(path, k), v)
		if err != nil {
			return nil, false, err
		}
		if changed && out == nil {
			out = make(map[string]any, len(m))
			for ok, ov := range m {
				out[ok] = ov
			}
		}
		if out != nil {
			out[k] = nv
		}
	}

	if out == nil {
		return m, false, nil
	}
	return out, true, nil
}

// resolveSlice resolves a slice copy-on-write, reporting whether anything changed.
func resolveSlice(ctx context.Context, path string, s []any) ([]any, bool, error) {
	var out []any
	for i, v := range s {
		nv, changed, err := resolveValue(ctx, joinIndex(path, i), v)
		if err != nil {
			return nil, false, err
		}
		if changed && out == nil {
			out = make([]any, len(s))
			copy(out, s)
		}
		if out != nil {
			out[i] = nv
		}
	}

	if out == nil {
		return s, false, nil
	}
	return out, true, nil
}

// resolveValue dispatches on the value's dynamic type, recursing into maps and slices.
func resolveValue(ctx context.Context, path string, v any) (any, bool, error) {
	switch val := v.(type) {
	case string:
		return resolveString(ctx, path, val)
	case map[string]any:
		return resolveDataMap(ctx, path, val)
	case []any:
		return resolveSlice(ctx, path, val)
	default:
		return v, false, nil
	}
}

// resolveString resolves placeholders in a single string value.
func resolveString(ctx context.Context, path, s string) (any, bool, error) {
	if !strings.Contains(s, "${") {
		return s, false, nil
	}

	if segs := parseSegments(strings.TrimSpace(s)); len(segs) == 1 && segs[0].isRef {
		return resolveWholeValue(ctx, path, segs[0])
	}

	return resolveInterpolation(ctx, path, s)
}

// resolveWholeValue resolves a value that is exactly one placeholder, applying
// the typed-default rules.
func resolveWholeValue(ctx context.Context, path string, seg segment) (any, bool, error) {
	value, found, err := lookupEnv(ctx, path, seg.name)
	if err != nil {
		return nil, false, err
	}

	if !seg.hasDefault {
		if !found {
			return nil, false, NewPlaceholderNotFoundError(path, seg.name)
		}
		return value, true, nil
	}

	typed := decodeScalar(seg.def)
	if !found {
		return typed, true, nil
	}

	coerced, err := coerceToKind(value, typed)
	if err != nil {
		return nil, false, NewPlaceholderCoercionError(path, seg.name, value, scalarKind(typed), err)
	}
	return coerced, true, nil
}

// resolveInterpolation resolves a string that mixes literal text and placeholders,
// producing a string result.
func resolveInterpolation(ctx context.Context, path, s string) (any, bool, error) {
	var b strings.Builder
	for _, seg := range parseSegments(s) {
		if !seg.isRef {
			b.WriteString(seg.literal)
			continue
		}

		value, found, err := lookupEnv(ctx, path, seg.name)
		if err != nil {
			return nil, false, err
		}
		switch {
		case found:
			b.WriteString(value)
		case seg.hasDefault:
			b.WriteString(seg.def)
		default:
			return nil, false, NewPlaceholderNotFoundError(path, seg.name)
		}
	}

	result := b.String()
	if result == s {
		return s, false, nil
	}
	return result, true, nil
}

// lookupEnv resolves a variable against the registry in ctx, mapping a
// not-found variable to (found=false) rather than an error.
func lookupEnv(ctx context.Context, path, name string) (string, bool, error) {
	reg := env.GetRegistry(ctx)
	if reg == nil {
		return "", false, ErrEnvRegistryMissing
	}

	value, found, err := reg.Lookup(ctx, name)
	if err != nil {
		if errors.Is(err, env.ErrVariableNotFound) {
			return "", false, nil
		}
		return "", false, NewPlaceholderLookupError(path, name, err)
	}
	return value, found, nil
}

// decodeScalar parses a default as a YAML scalar to determine its type; a quoted
// scalar stays a string and an empty default becomes an empty string.
func decodeScalar(def string) any {
	var out any
	if err := yaml.Unmarshal([]byte(def), &out); err != nil || out == nil {
		return def
	}
	switch out.(type) {
	case bool, int, int64, float64, string:
		return out
	default:
		return def
	}
}

// coerceToKind converts a string to the concrete type of the typed default.
func coerceToKind(value string, typed any) (any, error) {
	switch typed.(type) {
	case bool:
		return strconv.ParseBool(value)
	case int:
		n, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return nil, err
		}
		return int(n), nil
	case int64:
		return strconv.ParseInt(value, 10, 64)
	case float64:
		return strconv.ParseFloat(value, 64)
	default:
		return value, nil
	}
}

// scalarKind names the type of a typed default for error messages.
func scalarKind(typed any) string {
	switch typed.(type) {
	case bool:
		return "bool"
	case int, int64:
		return "int"
	case float64:
		return "float"
	default:
		return "string"
	}
}

// joinPath appends a map key to a field path.
func joinPath(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}

// joinIndex appends a slice index to a field path.
func joinIndex(path string, i int) string {
	return path + "[" + strconv.Itoa(i) + "]"
}
