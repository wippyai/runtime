// SPDX-License-Identifier: MPL-2.0

package stages

import (
	"context"
	"fmt"
	"strings"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/internal/entry"
)

const (
	sectionOverride boot.Name = "override"
)

type overrideStage struct{}

// Override creates a new stage that applies configuration overrides from boot config.
// Reads from the "override" config section and applies values to entries.
// Keys should be in format: namespace:entry:path (e.g., "app:gateway:addr")
// This format handles dots in namespace and entry names correctly.
func Override() boot.Stage {
	return &overrideStage{}
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

	sub := cfg.Sub(sectionOverride)
	keys := sub.Keys()

	if len(keys) == 0 {
		return nil
	}

	modifiedCount := 0
	var errs []error

	for _, key := range keys {
		value, ok := sub.Get(key)
		if !ok {
			continue
		}

		namespace, entryName, path, err := parseOverrideKey(key)
		if err != nil {
			errs = append(errs, NewInvalidKeyError(key, err))
			continue
		}

		targetEntries := findEntries(*entries, namespace, entryName)
		if len(targetEntries) == 0 {
			errs = append(errs, NewEntryNotFoundError(namespace, entryName))
			continue
		}

		for _, targetEntry := range targetEntries {
			if err := applyOverrideValue(mutator, targetEntry, path, value); err != nil {
				errs = append(errs, NewSetValueError(namespace, entryName, path, err))
				continue
			}
			modifiedCount++
		}
	}

	if len(errs) > 0 {
		return NewOverrideErrors(errs)
	}

	return nil
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

		target.Kind = registry.Kind(kind)
		return nil
	}

	return mutator.Set(target, path, value)
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
