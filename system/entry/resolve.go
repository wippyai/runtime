// SPDX-License-Identifier: MPL-2.0

package entry

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"github.com/wippyai/runtime/api/env"
	"github.com/wippyai/runtime/api/env/placeholder"
	"gopkg.in/yaml.v3"
)

// ExtractPlaceholderNames returns the variable names referenced by every
// recognized placeholder in s, in order of first appearance without duplicates.
func ExtractPlaceholderNames(s string) []string {
	return placeholder.ExtractNames(s)
}

// ResolveDataPlaceholders returns a copy of data with placeholders resolved
// against the environment registry in ctx. When no placeholder is present the
// input map is returned unchanged with no allocation. The input map is never
// mutated: copies are made only along paths that change.
func ResolveDataPlaceholders(ctx context.Context, data map[string]any) (map[string]any, error) {
	out, _, err := resolveDataMap(ctx, "", "", nil, data)
	return out, err
}

// resolveDataMap resolves a map copy-on-write, reporting whether anything
// changed. The logical path tracks the field position without slice indices and
// is matched against skip: fields a config type marks resolve:"-" (embedded
// source code and similar opaque content) pass through byte-identical.
func resolveDataMap(ctx context.Context, path, logical string, skip map[string]struct{}, m map[string]any) (map[string]any, bool, error) {
	if m == nil {
		return nil, false, nil
	}

	var out map[string]any
	for k, v := range m {
		nv, changed, err := resolveValue(ctx, joinPath(path, k), joinPath(logical, k), skip, v)
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
func resolveSlice(ctx context.Context, path, logical string, skip map[string]struct{}, s []any) ([]any, bool, error) {
	var out []any
	for i, v := range s {
		nv, changed, err := resolveValue(ctx, joinIndex(path, i), logical, skip, v)
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
func resolveValue(ctx context.Context, path, logical string, skip map[string]struct{}, v any) (any, bool, error) {
	if _, ok := skip[logical]; ok {
		return v, false, nil
	}

	switch val := v.(type) {
	case string:
		return resolveString(ctx, path, val)
	case map[string]any:
		return resolveDataMap(ctx, path, logical, skip, val)
	case []any:
		return resolveSlice(ctx, path, logical, skip, val)
	default:
		return v, false, nil
	}
}

// resolveString resolves placeholders in a single string value. Only a value
// that is exactly one placeholder — no surrounding text or whitespace — takes
// the typed whole-value path; anything else interpolates to a string.
func resolveString(ctx context.Context, path, s string) (any, bool, error) {
	if !strings.Contains(s, "${") {
		return s, false, nil
	}

	if segs := placeholder.Parse(s); len(segs) == 1 && segs[0].IsRef {
		return resolveWholeValue(ctx, path, segs[0])
	}

	return resolveInterpolation(ctx, path, s)
}

// resolveWholeValue resolves a value that is exactly one placeholder, applying
// the typed-default rules.
func resolveWholeValue(ctx context.Context, path string, seg placeholder.Segment) (any, bool, error) {
	value, found, err := lookupEnv(ctx, path, seg.Name)
	if err != nil {
		return nil, false, err
	}

	if !seg.HasDefault {
		if !found {
			return nil, false, NewPlaceholderNotFoundError(path, seg.Name)
		}
		return value, true, nil
	}

	typed := decodeScalar(seg.Default)
	if !found {
		return typed, true, nil
	}

	coerced, err := coerceToKind(value, typed)
	if err != nil {
		return nil, false, NewPlaceholderCoercionError(path, seg.Name, value, scalarKind(typed), err)
	}
	return coerced, true, nil
}

// resolveInterpolation resolves a string that mixes literal text and placeholders,
// producing a string result.
func resolveInterpolation(ctx context.Context, path, s string) (any, bool, error) {
	var b strings.Builder
	for _, seg := range placeholder.Parse(s) {
		if !seg.IsRef {
			b.WriteString(seg.Literal)
			continue
		}

		value, found, err := lookupEnv(ctx, path, seg.Name)
		if err != nil {
			return nil, false, err
		}
		switch {
		case found:
			b.WriteString(value)
		case seg.HasDefault:
			b.WriteString(seg.Default)
		default:
			return nil, false, NewPlaceholderNotFoundError(path, seg.Name)
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
