// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"

	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
)

// isRootDependency reports a deployment root. Deployment ingestion selects
// source-owned roots explicitly. A dependency authored through the registry API
// is owned by no module and is a user overlay root. Module-owned declarations
// that were not selected are transitive.
func isRootDependency(entry regapi.Entry, prov regapi.EntryProvenance) bool {
	return entry.Kind == regapi.NamespaceDependency && (prov.Root || prov.HostAuthored())
}

// collectControlledModules returns the dependency graph reachable from the
// state's deployment roots. A root's package owner is provenance, not part of
// that graph: the root controls its declared component, while ordinary owned
// dependencies extend the graph from owner to component.
func (h *DependencyHandler) collectControlledModules(
	ctx context.Context,
	snapshot regapi.ProvenancedState,
	transcoder payload.Transcoder,
) (map[string]struct{}, error) {
	controlled := make(map[string]struct{})
	dependencyLinks := make(map[string][]string)

	prov, err := stateProvenance(snapshot)
	if err != nil {
		return nil, err
	}

	for _, entry := range snapshot.Entries {
		if entry.Kind != regapi.NamespaceDependency {
			continue
		}
		record := prov[idKey(entry.ID)]
		def, err := decodeDependency(ctx, transcoder, entry)
		if err != nil {
			return nil, err
		}
		if def.Component == "" {
			return nil, NewDependencyEntryInvalidError(entry.ID.String(), "component is required", "")
		}

		if isRootDependency(entry, record) {
			controlled[def.Component] = struct{}{}
			continue
		}
		if record.Module != "" {
			dependencyLinks[record.Module] = append(dependencyLinks[record.Module], def.Component)
		}
	}

	queue := sortedSetKeys(controlled)
	for len(queue) > 0 {
		module := queue[0]
		queue = queue[1:]
		for _, dependency := range dependencyLinks[module] {
			if _, seen := controlled[dependency]; seen {
				continue
			}
			controlled[dependency] = struct{}{}
			queue = append(queue, dependency)
		}
	}

	return controlled, nil
}

// reconciliationControlledModules joins both sides of a version transition.
// Current roots retain authority over departing modules; target roots and the
// stored resolution cover retained and incoming modules. Package owners that
// are not dependency graph members remain outside reconciliation authority.
func (h *DependencyHandler) reconciliationControlledModules(
	ctx context.Context,
	current regapi.ProvenancedState,
	target regapi.ProvenancedState,
	transcoder payload.Transcoder,
	desired map[string]struct{},
) (map[string]struct{}, error) {
	controlled, err := h.collectControlledModules(ctx, current, transcoder)
	if err != nil {
		return nil, err
	}
	targetControlled, err := h.collectControlledModules(ctx, target, transcoder)
	if err != nil {
		return nil, err
	}
	addModuleSet(controlled, targetControlled)
	addModuleSet(controlled, desired)
	return controlled, nil
}

func addModuleSet(target, source map[string]struct{}) {
	for module := range source {
		target[module] = struct{}{}
	}
}
