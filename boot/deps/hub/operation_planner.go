// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"fmt"
	"reflect"
	"strings"

	regapi "github.com/wippyai/runtime/api/registry"
)

// entriesEqual compares author content only. Ownership and deployment-root
// selection are provenance, compared separately.
func entriesEqual(a, b regapi.Entry) bool {
	if !idsEqual(a.ID, b.ID) || a.Kind != b.Kind {
		return false
	}
	if !reflect.DeepEqual(a.Meta, b.Meta) {
		return false
	}
	switch {
	case a.Data == nil && b.Data == nil:
		return true
	case a.Data == nil || b.Data == nil:
		return false
	}
	if a.Data.Format() != b.Data.Format() {
		return false
	}
	return reflect.DeepEqual(a.Data.Data(), b.Data.Data())
}

type operationPlanOptions struct {
	controlledModules map[string]struct{}
	mutableModules    map[string]struct{}
	originalKey       string
}

type operationPlanner struct {
	resolver regapi.DependencyResolver
}

// plan diffs a provenanced current state against a provenanced desired state.
// Both provenance maps are total over their entries; a missing record is a hard
// error, so ownership is never inferred from an absent one.
func (p operationPlanner) plan(current, desired regapi.ProvenancedState, opts operationPlanOptions) ([]regapi.Operation, error) {
	currentProv, err := stateProvenance(current)
	if err != nil {
		return nil, err
	}
	desiredProv, err := stateProvenance(desired)
	if err != nil {
		return nil, err
	}

	currentByID := make(map[string]regapi.Entry, len(current.Entries))
	currentOwners := make(map[string]string, len(current.Entries))
	for _, entry := range current.Entries {
		key := idKey(entry.ID)
		currentByID[key] = entry
		currentOwners[key] = currentProv[key].Module
	}

	desiredByID := make(map[string]regapi.Entry, len(desired.Entries))
	for _, entry := range desired.Entries {
		desiredByID[idKey(entry.ID)] = entry
	}

	recreate, err := p.kindReplacementClosure(currentByID, desiredByID, currentOwners, opts)
	if err != nil {
		return nil, err
	}

	ops := make([]regapi.Operation, 0)
	originalKey := opts.originalKey
	for key, entry := range desiredByID {
		if key == originalKey {
			continue
		}
		desiredRecord := desiredProv[key]
		if existing, ok := currentByID[key]; ok {
			existingRecord := currentProv[key]
			if entryConflict(existingRecord, desiredRecord) {
				return nil, NewDependencyEntryConflictError(entry.ID.String(), existingRecord.Module, desiredRecord.Module)
			}
			if _, replace := recreate[key]; replace {
				ops = append(ops,
					regapi.Operation{Kind: regapi.EntryDelete, Entry: existing, Provenance: provenancePtr(existingRecord)},
					regapi.Operation{Kind: regapi.EntryCreate, Entry: entry, Provenance: provenancePtr(desiredRecord)},
				)
				continue
			}
			// A deployment-root transition changes no author content: it is a
			// provenance delta and still produces exactly one operation.
			if !entriesEqual(existing, entry) || existingRecord.Root != desiredRecord.Root {
				if preserveImmutableResidentEntry(existingRecord, desiredRecord, opts.mutableModules) {
					continue
				}
				ops = append(ops, regapi.Operation{Kind: regapi.EntryUpdate, Entry: entry, Provenance: provenancePtr(desiredRecord)})
			}
		} else {
			ops = append(ops, regapi.Operation{Kind: regapi.EntryCreate, Entry: entry, Provenance: provenancePtr(desiredRecord)})
		}
	}

	for key, entry := range currentByID {
		if key == originalKey {
			continue
		}
		if _, ok := desiredByID[key]; ok {
			continue
		}
		if module := currentOwners[key]; module != "" {
			if opts.controlledModules != nil {
				if _, ok := opts.controlledModules[module]; !ok {
					continue
				}
			}
			if hasLiveDependent(entry.ID, currentByID, desiredByID, currentOwners, opts.controlledModules, p.resolver) {
				continue
			}
			record := currentProv[key]
			ops = append(ops, regapi.Operation{
				Kind:       regapi.EntryDelete,
				Entry:      regapi.Entry{ID: entry.ID},
				Provenance: provenancePtr(record),
			})
		}
	}

	return ops, nil
}

func provenancePtr(record regapi.EntryProvenance) *regapi.EntryProvenance {
	return &record
}

// kindReplacementClosure returns the entries that must be recreated when an
// entry changes kind. Registry kinds are immutable, so the provider must be
// deleted and created. Every still-live dependent must cross that boundary in
// the same changeset; otherwise the delete would temporarily invalidate the
// resident graph.
func (p operationPlanner) kindReplacementClosure(
	currentByID map[string]regapi.Entry,
	desiredByID map[string]regapi.Entry,
	currentOwners map[string]string,
	opts operationPlanOptions,
) (map[string]struct{}, error) {
	recreate := make(map[string]struct{})
	for key, current := range currentByID {
		if desired, ok := desiredByID[key]; ok && current.Kind != desired.Kind {
			recreate[key] = struct{}{}
		}
	}
	if len(recreate) == 0 || p.resolver == nil {
		return recreate, nil
	}

	universe := dependencyEntryUniverse(currentByID, desiredByID)

	for changed := true; changed; {
		changed = false
		for key, current := range currentByID {
			if _, already := recreate[key]; already {
				continue
			}
			effective, present := desiredByID[key]
			if !present {
				if missingDesiredEntryWillBeDeleted(currentOwners[key], opts.controlledModules) {
					continue
				}
				effective = current
			}
			if !entryDependsOnSet(effective, recreate, universe, p.resolver) {
				continue
			}
			if !present {
				return nil, fmt.Errorf("cannot replace registry entry kind for live dependent %s absent from desired state", current.ID.String())
			}
			if key == opts.originalKey {
				return nil, fmt.Errorf("cannot replace registry entry kind for original operation target %s", current.ID.String())
			}
			recreate[key] = struct{}{}
			changed = true
		}
	}
	return recreate, nil
}

func entryDependsOnSet(
	entry regapi.Entry,
	targets map[string]struct{},
	universe dependencyEntryUniverseView,
	resolver regapi.DependencyResolver,
) bool {
	dependencies := append([]string(nil), entry.Meta.GetSlice(regapi.TagDependsOn)...)
	dependencies = append(dependencies, resolver.Extract(entry)...)
	for _, dep := range dependencies {
		switch depType, value := parseDependencyRef(dep); depType {
		case "direct":
			if _, ok := targets[idKey(resolveDependencyRef(entry.ID.NS, value))]; ok {
				return true
			}
		case "group":
			for _, id := range universe.groups[value] {
				if _, ok := targets[idKey(id)]; ok {
					return true
				}
			}
		case "namespace":
			for _, id := range universe.ns[value] {
				if _, ok := targets[idKey(id)]; ok {
					return true
				}
			}
		}
	}
	return false
}

func hasLiveDependent(
	target regapi.ID,
	currentByID map[string]regapi.Entry,
	desiredByID map[string]regapi.Entry,
	currentOwners map[string]string,
	controlledModules map[string]struct{},
	resolver regapi.DependencyResolver,
) bool {
	if resolver == nil {
		return false
	}
	universe := dependencyEntryUniverse(currentByID)
	targetKey := idKey(target)
	for key, current := range currentByID {
		if key == targetKey {
			continue
		}
		check := current
		if desired, ok := desiredByID[key]; ok {
			check = desired
		} else if missingDesiredEntryWillBeDeleted(currentOwners[key], controlledModules) {
			continue
		}
		if entryDependsOn(check, target, universe, resolver) {
			return true
		}
	}
	return false
}

func missingDesiredEntryWillBeDeleted(module string, controlledModules map[string]struct{}) bool {
	if module == "" {
		return false
	}
	if controlledModules == nil {
		return true
	}
	_, ok := controlledModules[module]
	return ok
}

type dependencyEntryUniverseView struct {
	groups map[string][]regapi.ID
	ns     map[string][]regapi.ID
}

func dependencyEntryUniverse(entrySets ...map[string]regapi.Entry) dependencyEntryUniverseView {
	universe := dependencyEntryUniverseView{
		groups: make(map[string][]regapi.ID),
		ns:     make(map[string][]regapi.ID),
	}
	for _, entries := range entrySets {
		for _, entry := range entries {
			for _, group := range entry.Meta.GetSlice(regapi.TagGroups) {
				universe.groups[group] = append(universe.groups[group], entry.ID)
			}
			if entry.ID.NS != "" {
				universe.ns[entry.ID.NS] = append(universe.ns[entry.ID.NS], entry.ID)
			}
		}
	}
	return universe
}

func entryDependsOn(entry regapi.Entry, target regapi.ID, universe dependencyEntryUniverseView, resolver regapi.DependencyResolver) bool {
	dependencies := entry.Meta.GetSlice(regapi.TagDependsOn)
	dependencies = append(dependencies, resolver.Extract(entry)...)
	for _, dep := range dependencies {
		switch depType, value := parseDependencyRef(dep); depType {
		case "direct":
			if resolveDependencyRef(entry.ID.NS, value) == target {
				return true
			}
		case "group":
			for _, id := range universe.groups[value] {
				if id == target {
					return true
				}
			}
		case "namespace":
			for _, id := range universe.ns[value] {
				if id == target {
					return true
				}
			}
		}
	}
	return false
}

func parseDependencyRef(dep string) (depType string, value string) {
	if strings.HasPrefix(dep, "group:") {
		return "group", strings.TrimPrefix(dep, "group:")
	}
	if strings.HasPrefix(dep, "ns:") {
		return "namespace", strings.TrimPrefix(dep, "ns:")
	}
	return "direct", dep
}

func resolveDependencyRef(sourceNS string, dep string) regapi.ID {
	if strings.Contains(dep, ":") {
		return regapi.ParseID(dep)
	}
	return regapi.NewID(sourceNS, dep)
}

// preserveImmutableResidentEntry prevents normalization from rewriting an
// installed artifact that was not selected as mutable. The comparison is
// per-entry resident identity: the same module at the same resident version.
func preserveImmutableResidentEntry(existing, desired regapi.EntryProvenance, mutableModules map[string]struct{}) bool {
	if mutableModules == nil {
		return false
	}
	if existing.Root != desired.Root {
		return false
	}
	if desired.Module == "" || desired.Module != existing.Module {
		return false
	}
	if _, mutable := mutableModules[desired.Module]; mutable {
		return false
	}
	if existing.Version != "" && desired.Version != "" && existing.Version != desired.Version {
		return false
	}
	return true
}

// entryConflict reports a module claiming an id that another module or the host
// already owns. Adoption of a host entry by a module is not a flow.
func entryConflict(existing, desired regapi.EntryProvenance) bool {
	if desired.Module == "" {
		return false
	}
	return existing.Module == "" || existing.Module != desired.Module
}
