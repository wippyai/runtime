// SPDX-License-Identifier: MPL-2.0

package stages

import (
	"context"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/internal/entry"
)

// ModuleDefinition represents the data structure of an ns.definition entry.
// This entry is required for publishing and contains module metadata.
// Release notes are NOT part of definition - they're provided at publish time.
type ModuleDefinition struct {
	Module string `json:"module" yaml:"module"` // Module display name (defaults to entry name if empty)
	Readme string `json:"readme" yaml:"readme"`
}

// FindDefinition finds the ns.definition entry in the entries slice.
// Returns nil if not found.
func FindDefinition(entries []registry.Entry) *registry.Entry {
	for i := range entries {
		if entries[i].Kind == registry.NamespaceDefinition {
			return &entries[i]
		}
	}
	return nil
}

// FindDefinitions finds all ns.definition entries in the entries slice.
func FindDefinitions(entries []registry.Entry) []registry.Entry {
	var defs []registry.Entry
	for _, e := range entries {
		if e.Kind == registry.NamespaceDefinition {
			defs = append(defs, e)
		}
	}
	return defs
}

// DecodeDefinition decodes an ns.definition entry into ModuleDefinition.
func DecodeDefinition(ctx context.Context, transcoder payload.Transcoder, e registry.Entry) (*ModuleDefinition, error) {
	return entry.DecodeEntryConfig[ModuleDefinition](ctx, transcoder, e)
}

// ValidateDefinitionForPublish validates that entries contain exactly one ns.definition.
func ValidateDefinitionForPublish(entries []registry.Entry) error {
	defs := FindDefinitions(entries)

	if len(defs) == 0 {
		return ErrNoDefinition
	}

	if len(defs) > 1 {
		return NewMultipleDefinitionsError(len(defs))
	}

	return nil
}
