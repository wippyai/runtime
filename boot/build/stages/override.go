// SPDX-License-Identifier: MPL-2.0

package stages

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/system/entry"
)

const (
	sectionOverride boot.Name = "override"
)

type OverrideOption func(*overrideStage)

type overrideStage struct {
	ignoreMissingEntries bool
}

// overrideValue retains its authored path. The logical catalog path is used
// during collection only: preserving the physical spelling keeps admission's
// legacy diagnostics intact and avoids creating a duplicate group.
type overrideValue struct {
	value     any
	target    *registry.Entry
	namespace string
	entryName string
	path      string
}

func WithMissingOverrideEntriesIgnored() OverrideOption {
	return func(s *overrideStage) {
		s.ignoreMissingEntries = true
	}
}

// Override creates a new stage that applies configuration overrides from boot config.
// Reads from the "override" config section and applies values to entries.
// Keys should be in format: namespace:entry:path (e.g., "app:gateway:addr")
// This format handles dots in namespace and entry names correctly.
func Override(opts ...OverrideOption) boot.Stage {
	stage := &overrideStage{}
	for _, opt := range opts {
		if opt != nil {
			opt(stage)
		}
	}
	return stage
}

func (s *overrideStage) Name() string {
	return "override"
}

func (s *overrideStage) Execute(ctx context.Context, entries *[]registry.Entry) error {
	cfg := boot.GetConfig(ctx)
	if cfg == nil {
		return nil
	}

	transcoder := payload.GetTranscoder(ctx)
	if transcoder == nil {
		return ErrTranscoderNotFound
	}

	mutator := entry.NewMutator(transcoder)

	values, errs := s.collectOverrides(cfg, *entries)
	if len(errs) > 0 {
		return NewOverrideErrors(errs)
	}

	prepared := make(map[*registry.Entry]struct{})
	for _, override := range values {
		if _, ok := prepared[override.target]; ok {
			continue
		}
		if err := prepareOptionMutationTarget(override.target, transcoder); err != nil {
			return NewOverrideErrors([]error{NewSetValueError(override.namespace, override.entryName, override.path, err)})
		}
		prepared[override.target] = struct{}{}
	}

	for _, override := range values {
		path := override.path
		if err := applyOverrideValue(mutator, override.target, path, override.value); err != nil {
			errs = append(errs, NewSetValueError(override.namespace, override.entryName, path, err))
		}
	}

	if len(errs) > 0 {
		return NewOverrideErrors(errs)
	}

	return nil
}

// collectOverrides reads each original configuration input in precedence
// order. The resolved Config alone cannot tell whether two spellings came
// from one declaration or from two layers, which matters for alias conflicts.
// The API catalog provides logical identities only; writes retain an existing
// declaration spelling so legacy diagnostics remain available to admission.
func (s *overrideStage) collectOverrides(cfg boot.Config, entries []registry.Entry) ([]overrideValue, []error) {
	var values []overrideValue
	var errs []error
	kinds := make(map[*registry.Entry]registry.Kind)

	for _, layer := range boot.ConfigLayers(cfg.Sub(sectionOverride)) {
		keys := layer.Keys()
		sortOverrideKeys(keys)
		seen := make(map[*registry.Entry]map[string]map[string][]string)

		for _, key := range keys {
			value, ok := layer.Get(key)
			if !ok {
				continue
			}

			namespace, entryName, path, err := parseOverrideKey(key)
			if err != nil {
				errs = append(errs, NewInvalidKeyError(key, err))
				continue
			}

			targetEntries := findEntries(entries, namespace, entryName)
			if len(targetEntries) == 0 {
				if !s.ignoreMissingEntries {
					errs = append(errs, NewEntryNotFoundError(namespace, entryName))
				}
				continue
			}

			for _, target := range targetEntries {
				kind := target.Kind
				if overriddenKind, ok := kinds[target]; ok {
					kind = overriddenKind
				}
				expandedValue, expandErr := expandTypedOptionContainer(kind, path, value)
				if expandErr != nil {
					errs = append(errs, NewSetValueError(namespace, entryName, path, expandErr))
					continue
				}
				conflictFound := false
				for _, control := range optionControls(kind, path, expandedValue) {
					info := control.Info
					if conflictingPath, conflict := conflictingAliasPath(seen[target], info); conflict {
						errs = append(errs, NewOverrideAliasConflictError(namespace, entryName, path, conflictingPath))
						conflictFound = true
						break
					}
					rememberAliasPath(seen, target, info)
				}
				if conflictFound {
					continue
				}

				if isKindOverride(path) {
					newKind, kindErr := overrideKind(value)
					if kindErr != nil {
						errs = append(errs, NewSetValueError(namespace, entryName, path, kindErr))
						continue
					}
					kinds[target] = newKind
				}

				values = append(values, overrideValue{
					namespace: namespace,
					entryName: entryName,
					path:      path,
					value:     expandedValue,
					target:    target,
				})
			}
		}
	}

	return values, errs
}

func sortOverrideKeys(keys []string) {
	sort.SliceStable(keys, func(i, j int) bool {
		leftKind, rightKind := isKindOverrideKey(keys[i]), isKindOverrideKey(keys[j])
		if leftKind != rightKind {
			return leftKind
		}
		return keys[i] < keys[j]
	})
}

func isKindOverrideKey(key string) bool {
	_, _, path, err := parseOverrideKey(key)
	return err == nil && isKindOverride(path)
}

func isKindOverride(path string) bool {
	return strings.TrimPrefix(path, ".") == "kind"
}

func overrideKind(value any) (string, error) {
	kind, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("kind override must be a string, got %T", value)
	}
	kind = strings.TrimSpace(kind)
	if kind == "" {
		return "", fmt.Errorf("kind override must not be empty")
	}
	return kind, nil
}

func applyOverrideValue(mutator *entry.Mutator, target *registry.Entry, path string, value any) error {
	normalizedPath := strings.TrimPrefix(path, ".")
	if normalizedPath == "kind" {
		kind, ok := value.(string)
		if !ok {
			return fmt.Errorf("kind override must be a string, got %T", value)
		}

		kind = strings.TrimSpace(kind)
		if kind == "" {
			return fmt.Errorf("kind override must not be empty")
		}

		target.Kind = kind
		return nil
	}

	return setOptionValue(mutator, target, path, value)
}

// parseOverrideKey parses a key in format "namespace:entry:path" into components.
// Uses two colons to properly handle dots in namespace and entry names.
// Examples:
//   - "app:gateway:addr" -> ("app", "gateway", "addr", nil)
//   - "app:gateway:data.addr" -> ("app", "gateway", "data.addr", nil)
//   - "app.v2:gateway.v1:addr" -> ("app.v2", "gateway.v1", "addr", nil)
//   - "db:main:meta.priority" -> ("db", "main", "meta.priority", nil)
func parseOverrideKey(key string) (namespace, entryName, path string, err error) {
	if key == "" {
		return "", "", "", ErrEmptyKey
	}

	firstColonIdx := strings.Index(key, ":")
	if firstColonIdx == -1 {
		return "", "", "", NewMissingSeparatorError("first ':'", "namespace:entry:path")
	}

	namespace = key[:firstColonIdx]
	remainder := key[firstColonIdx+1:]

	if namespace == "" {
		return "", "", "", ErrEmptyNamespace
	}

	if remainder == "" {
		return "", "", "", NewMissingFieldError("entry name and path")
	}

	entryName, path, _ = strings.Cut(remainder, ":")

	if entryName == "" {
		return "", "", "", ErrEmptyEntryName
	}

	if path == "" {
		return "", "", "", ErrEmptyPath
	}

	return namespace, entryName, path, nil
}

// findEntries finds all entries matching the given namespace and name
func findEntries(entries []registry.Entry, namespace, name string) []*registry.Entry {
	var results []*registry.Entry

	for i := range entries {
		e := &entries[i]
		if e.ID.NS == namespace && e.ID.Name == name {
			results = append(results, e)
		}
	}

	return results
}
