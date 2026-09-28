// SPDX-License-Identifier: MPL-2.0

package wasm

import (
	"fmt"
	"sort"

	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/registry"
)

// DeprecatedOptionPath records an accepted legacy spelling. Admission owns
// warning policy; API configuration types only retain structured provenance.
type DeprecatedOptionPath struct {
	Path        string
	Replacement string
}

type optionSource struct {
	value      any
	path       string
	present    bool
	deprecated bool
	strictNull bool
}

// optionGroupCatalog is the only source of path aliases for WASM entry
// options. Future mutator/override wiring must call CanonicalOptionPath rather
// than infer aliases from strings.
type optionGroupSpec struct {
	canonical string
	flat      string
	flatWarn  bool
}

var wasmOptionGroups = map[registry.Kind]map[string]optionGroupSpec{
	FunctionWAT: {
		"pool":   {canonical: "pool", flat: "pool"}, // public migration deferred
		"limits": {canonical: "options.limits", flat: "limits", flatWarn: true},
	},
	FunctionWASM: {
		"pool":   {canonical: "pool", flat: "pool"}, // public migration deferred
		"limits": {canonical: "options.limits", flat: "limits", flatWarn: true},
	},
	ProcessWASM: {
		"limits":       {canonical: "options.limits", flat: "limits", flatWarn: true},
		"mailbox":      {canonical: "options.mailbox"},
		"worker_class": {canonical: "options.worker_class"},
	},
}

// OptionPathInfo identifies a control path without inspecting a declaration's
// values. GroupAliases is an owned slice of accepted authored group paths.
// Admission uses this identity to distinguish aliases from independent leaves.
type OptionPathInfo struct {
	CanonicalPath  string
	CanonicalGroup string
	AuthoredGroup  string
	Suffix         string
	GroupAliases   []string
}

// ResolveOptionPath resolves only paths declared by the per-kind catalog.
// Paths use authored entry coordinates: meta refers to entry metadata, while
// the caller must handle an explicit data prefix before calling this function.
func ResolveOptionPath(kind registry.Kind, path string) (OptionPathInfo, bool) {
	groups, ok := wasmOptionGroups[kind]
	if !ok {
		return OptionPathInfo{}, false
	}
	for group, spec := range groups {
		aliases := []string{"options." + group, "meta.options." + group}
		if spec.flat != "" {
			aliases = append(aliases, spec.flat)
		}
		for _, prefix := range aliases {
			if path == prefix || (len(path) > len(prefix) && path[:len(prefix)] == prefix && path[len(prefix)] == '.') {
				suffix := path[len(prefix):]
				return OptionPathInfo{CanonicalPath: spec.canonical + suffix, CanonicalGroup: spec.canonical, AuthoredGroup: prefix, Suffix: suffix, GroupAliases: aliases}, true
			}
		}
	}
	return OptionPathInfo{}, false
}

// CanonicalOptionPath reports the canonical authored path for a supported
// WASM option group. It intentionally has no heuristic fallback.
func CanonicalOptionPath(kind registry.Kind, path string) (string, bool) {
	resolved, ok := ResolveOptionPath(kind, path)
	return resolved.CanonicalPath, ok
}

func groupCatalog(kind registry.Kind) (map[string]struct{}, map[string]optionGroupSpec, error) {
	specs, ok := wasmOptionGroups[kind]
	if !ok {
		return nil, nil, apierror.New(apierror.Invalid, fmt.Sprintf("unsupported WASM options kind %q", kind)).WithRetryable(apierror.False)
	}
	allowed := make(map[string]struct{}, len(specs))
	for group := range specs {
		allowed[group] = struct{}{}
	}
	return allowed, specs, nil
}

func optionGroups(kind registry.Kind, raw any, present bool, path string, allowed map[string]struct{}, allowUnknown bool) (map[string]any, error) {
	if !present {
		return nil, nil
	}
	m, err := ExpandOptionObject(kind, raw, path)
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	filtered := make(map[string]any, len(keys))
	for _, key := range keys {
		if _, ok := allowed[key]; !ok {
			if allowUnknown {
				continue
			}
			return nil, newUnknownFieldError(path, key)
		}
		filtered[key] = m[key]
	}
	return filtered, nil
}

// ExpandOptionObject gives admission stages the same object view used by
// option validation, including programmatically supplied typed options.
// It preserves authored group spellings and does not filter custom keys.
// Map inputs are borrowed and must not be mutated; typed inputs produce a new
// map only after validation, so serialization cannot hide invalid values.
func ExpandOptionObject(kind registry.Kind, raw any, path string) (map[string]any, error) {
	if raw == nil {
		return nil, apierror.New(apierror.Invalid, fmt.Sprintf("%s must be an object", path)).WithRetryable(apierror.False)
	}
	var m map[string]any
	switch v := raw.(type) {
	case attrs.Bag:
		m = map[string]any(v)
	case map[string]any:
		m = v
	case FunctionOptions:
		if kind != FunctionWAT && kind != FunctionWASM {
			return nil, ErrProcessOptionsInvalidType
		}
		validated, err := validateFunctionOptionsStruct(v)
		if err != nil {
			return nil, err
		}
		m = serializeFunctionOptions(validated)
	case *FunctionOptions:
		if kind != FunctionWAT && kind != FunctionWASM {
			return nil, ErrProcessOptionsInvalidType
		}
		if v == nil {
			if path == "options" {
				return nil, apierror.New(apierror.Invalid, "options must be an object").WithRetryable(apierror.False)
			}
			return nil, nil
		}
		validated, err := validateFunctionOptionsStruct(*v)
		if err != nil {
			return nil, err
		}
		m = serializeFunctionOptions(validated)
	case ProcessOptions:
		if kind != ProcessWASM {
			return nil, ErrFunctionOptionsInvalidType
		}
		validated, err := validateOptionsStruct(v)
		if err != nil {
			return nil, err
		}
		m = serializeProcessOptions(validated)
	case *ProcessOptions:
		if kind != ProcessWASM {
			return nil, ErrFunctionOptionsInvalidType
		}
		if v == nil {
			if path == "options" {
				return nil, apierror.New(apierror.Invalid, "options must be an object").WithRetryable(apierror.False)
			}
			return nil, nil
		}
		validated, err := validateOptionsStruct(*v)
		if err != nil {
			return nil, err
		}
		m = serializeProcessOptions(validated)
	default:
		return nil, apierror.New(apierror.Invalid, fmt.Sprintf("%s must be an object", path)).WithRetryable(apierror.False)
	}
	return m, nil
}

// normalizeOptionGroups accepts the canonical root options object and the
// compatibility spellings. It does not mutate any input map. Different
// logical groups may come from different layers; the same group may not.
func normalizeOptionGroups(kind registry.Kind, canonical optionSource, meta attrs.Bag, flat []optionSource, allowed map[string]struct{}, canonicalPaths map[string]string, metaAllowsUnknown bool) (map[string]any, []DeprecatedOptionPath, error) {
	sources := []optionSource{canonical}
	if meta != nil {
		if v, ok := meta.Get("options"); ok && v != nil {
			sources = append(sources, optionSource{path: "meta.options", value: v, present: true, deprecated: true})
		}
	}
	sources = append(sources, flat...)

	type groupValue struct {
		group  string
		value  any
		source optionSource
	}
	var candidates []groupValue
	seen := make(map[string]string)
	for _, source := range sources {
		if !source.present {
			continue
		}
		groups, err := optionGroups(kind, source.value, true, source.path, allowed, source.path == "meta.options" && metaAllowsUnknown)
		if err != nil {
			return nil, nil, err
		}
		keys := make([]string, 0, len(groups))
		for group := range groups {
			keys = append(keys, group)
		}
		sort.Strings(keys)
		for _, group := range keys {
			value := groups[group]
			if prior, exists := seen[group]; exists {
				return nil, nil, apierror.New(apierror.Invalid, fmt.Sprintf("duplicate options.%s declared at %s and %s", group, prior, source.path)).WithRetryable(apierror.False)
			}
			seen[group] = source.path
			candidates = append(candidates, groupValue{group: group, value: value, source: source})
		}
	}

	resolved := make(map[string]any)
	var deprecated []DeprecatedOptionPath
	for _, candidate := range candidates {
		if candidate.value == nil && candidate.source.strictNull {
			return nil, nil, apierror.New(apierror.Invalid, fmt.Sprintf("%s.%s must not be null", candidate.source.path, candidate.group)).WithRetryable(apierror.False)
		}
		if candidate.value == nil {
			continue
		}
		resolved[candidate.group] = candidate.value
		if candidate.source.deprecated {
			path := candidate.source.path
			if path != candidate.group {
				path += "." + candidate.group
			}
			deprecated = append(deprecated, DeprecatedOptionPath{Path: path, Replacement: canonicalPaths[candidate.group]})
		}
	}
	return resolved, deprecated, nil
}

// NormalizeEntryOptions is a pure normalizer intended for admission/merge
// callers. Data and meta are read only. It returns one canonical group map and
// structured legacy provenance; it performs neither logging nor configuration
// mutation.
func NormalizeEntryOptions(kind registry.Kind, data map[string]any, meta attrs.Bag) (map[string]any, []DeprecatedOptionPath, error) {
	allowed, specs, err := groupCatalog(kind)
	if err != nil {
		return nil, nil, err
	}
	var rootOptions any
	rootOptionsPresent := false
	if data != nil {
		rootOptions, rootOptionsPresent = data["options"]
	}
	paths := make(map[string]string, len(specs))
	flat := make([]optionSource, 0, len(specs))
	for group, spec := range specs {
		paths[group] = spec.canonical
		if spec.flat == "" || data == nil {
			continue
		}
		value, present := data[spec.flat]
		flat = append(flat, optionSource{path: spec.flat, value: map[string]any{group: value}, present: present, deprecated: spec.flatWarn})
	}
	metaAllowsUnknown := kind == FunctionWAT || kind == FunctionWASM
	return normalizeOptionGroups(kind, optionSource{path: "options", value: rootOptions, present: rootOptionsPresent, strictNull: true}, meta, flat, allowed, paths, metaAllowsUnknown)
}

func cloneMetaWithoutControlOptions(meta attrs.Bag, kind registry.Kind) attrs.Bag {
	if meta == nil {
		return nil
	}
	clone := make(attrs.Bag, len(meta))
	for k, v := range meta {
		if k != "options" {
			clone[k] = v
			continue
		}
		if typedOptionValueForKind(kind, v) {
			// Typed legacy controls have no interceptor fields. SetOptions
			// replaces them with root options just as it replaces map controls.
			continue
		}
		allowed, _, err := groupCatalog(kind)
		if err != nil {
			clone[k] = v
			continue
		}
		groups, ok := v.(map[string]any)
		if !ok {
			if bag, yes := v.(attrs.Bag); yes {
				groups = map[string]any(bag)
				ok = true
			}
		}
		if !ok {
			clone[k] = v
			continue
		}
		kept := make(map[string]any, len(groups))
		for option, value := range groups {
			if _, control := allowed[option]; !control {
				kept[option] = value
			}
		}
		if len(kept) > 0 {
			clone[k] = kept
		}
	}
	return clone
}

func typedOptionValueForKind(kind registry.Kind, value any) bool {
	switch value.(type) {
	case FunctionOptions, *FunctionOptions:
		return kind == FunctionWAT || kind == FunctionWASM
	case ProcessOptions, *ProcessOptions:
		return kind == ProcessWASM
	default:
		return false
	}
}
