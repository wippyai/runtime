// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"
	"encoding/json"

	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
)

func resolutionBindingIndex(resolution *regapi.DependencyResolution) map[string]regapi.DependencyBinding {
	bindings := make(map[string]regapi.DependencyBinding)
	if resolution != nil {
		for _, binding := range resolution.Bindings {
			bindings[binding.ID] = binding
		}
	}
	return bindings
}

// bindingDependencies supplies checkpointed parameters to linking without
// rewriting the authored declarations used by resolution or stored in history.
func bindingDependencies(ctx context.Context, state regapi.State, bindings map[string]regapi.DependencyBinding, transcoder payload.Transcoder) ([]regapi.Entry, error) {
	if len(bindings) == 0 {
		return nil, nil
	}
	var entries []regapi.Entry
	for _, entry := range state {
		binding, recorded := bindings[entry.ID.String()]
		if !recorded || entry.Kind != regapi.NamespaceDependency {
			continue
		}
		definition, err := decodeDependency(ctx, transcoder, entry)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(binding.Parameters, &definition.Parameters); err != nil {
			return nil, NewDependencyLoadError(entry.ID.String(), err)
		}
		entry.Data = payload.New(definition)
		entries = append(entries, entry)
	}
	return entries, nil
}

// Retain exact linking decisions for unchanged declarations only. Replacing
// their package owner releases old bindings alongside its old closure pins.
// A new authored operation supplies a new decision, including an empty clear.
func retainedBindings(current *regapi.DependencyResolution, snapshot, materialized regapi.State, authored regapi.ChangeSet) map[string]regapi.DependencyBinding {
	bindings := resolutionBindingIndex(current)
	if len(bindings) == 0 {
		return bindings
	}
	replacedOwners := make(map[string]struct{})
	for _, entry := range materialized {
		if owner := entryModule(entry); owner != "" {
			replacedOwners[owner] = struct{}{}
		}
	}
	for _, entry := range snapshot {
		if _, replaced := replacedOwners[entryModule(entry)]; replaced {
			delete(bindings, entry.ID.String())
		}
	}
	for _, operation := range authored {
		delete(bindings, operation.Entry.ID.String())
	}
	return bindings
}

func recordBindings(ctx context.Context, resolution *regapi.DependencyResolution, retained map[string]regapi.DependencyBinding, authored []regapi.Entry, transcoder payload.Transcoder) error {
	for _, entry := range authored {
		if entry.Kind != regapi.NamespaceDependency {
			continue
		}
		definition, err := decodeDependency(ctx, transcoder, entry)
		if err != nil {
			return err
		}
		parameters := definition.Parameters
		if parameters == nil {
			parameters = []Parameter{}
		}
		encoded, err := json.Marshal(parameters)
		if err != nil {
			return NewDependencyLoadError(entry.ID.String(), err)
		}
		retained[entry.ID.String()] = regapi.DependencyBinding{ID: entry.ID.String(), Parameters: encoded}
	}
	resolution.Bindings = nil
	for _, root := range append(append([]regapi.DependencyRoot(nil), resolution.Roots...), resolution.References...) {
		if binding, recorded := retained[root.ID]; recorded {
			resolution.Bindings = append(resolution.Bindings, binding)
		}
	}
	return nil
}

// A parameter-only update may not reload the declaration's package owner.
// Read that selected artifact, never a previously authored snapshot, to recover
// its defaults. Only requested owners are loaded, once each, and only structural
// headers from resident state supplement the requirement-address index.
func (h *DependencyHandler) bindingArtifacts(ctx context.Context, resolved []ResolvedModule, materialized, snapshot regapi.State, authored regapi.ChangeSet, transcoder payload.Transcoder) (regapi.State, *unpackPlan, error) {
	if len(authored) == 0 {
		return nil, nil, nil
	}
	loadedOwners := make(map[string]struct{})
	for _, entry := range materialized {
		loadedOwners[entryModule(entry)] = struct{}{}
	}
	missingOwners := make(map[string]struct{})
	for _, operation := range authored {
		if operation.Kind == regapi.EntryDelete {
			continue
		}
		if owner := entryModule(operation.Entry); owner != "" {
			if _, loaded := loadedOwners[owner]; !loaded {
				missingOwners[owner] = struct{}{}
			}
		}
	}
	additional, plan, err := h.loadModuleEntries(ctx, filterResolvedModules(resolved, missingOwners), nil, transcoder)
	if err != nil {
		return nil, nil, err
	}
	for _, entry := range additional {
		loadedOwners[entryModule(entry)] = struct{}{}
	}
	byID := make(regapi.StateMap)
	for _, entry := range snapshot {
		if owner := entryModule(entry); owner != "" {
			if _, reloaded := loadedOwners[owner]; reloaded {
				continue
			}
		}
		if entry.Kind == regapi.NamespaceDefinition || entry.Kind == regapi.NamespaceRequirement {
			byID[entry.ID] = entry
		}
	}
	for _, entry := range additional {
		byID[entry.ID] = entry
	}
	for _, entry := range materialized {
		byID[entry.ID] = entry
	}
	entries := make(regapi.State, 0, len(byID))
	for _, entry := range byID {
		entries = append(entries, entry)
	}
	return entries, plan, nil
}
