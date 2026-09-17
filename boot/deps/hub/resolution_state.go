// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
)

type desiredDependency struct {
	entry      regapi.Entry
	definition DependencyDefinition
}

// applyOperationToState materializes the state an operation produces, so a
// recorded graph can bind to the deployment identity of its own version.
func applyOperationToState(snapshot regapi.State, op regapi.Operation) regapi.State {
	next := make(regapi.State, 0, len(snapshot)+1)
	replaced := false
	for _, entry := range snapshot {
		if idsEqual(entry.ID, op.Entry.ID) {
			if op.Kind == regapi.EntryCreate || op.Kind == regapi.EntryUpdate {
				next = append(next, op.Entry)
				replaced = true
			}
			continue
		}
		next = append(next, entry)
	}
	if !replaced && (op.Kind == regapi.EntryCreate || op.Kind == regapi.EntryUpdate) {
		next = append(next, op.Entry)
	}
	return next
}
func rootExpansionDriver(op regapi.Operation, snapshot regapi.State) (regapi.Operation, bool) {
	if entry, ok := resolveOperationEntry(op, snapshot); ok && isRootDependency(entry) {
		return op, true
	}
	for _, entry := range snapshot {
		if idsEqual(entry.ID, op.Entry.ID) && isRootDependency(entry) {
			return regapi.Operation{Kind: regapi.EntryDelete, Entry: entry}, true
		}
	}
	return regapi.Operation{}, false
}

// refreshResolvedModules re-resolves the final declarations for a graph whose
// stored selection cannot be replayed, seeding the solver with stored,
// installed, and locked versions so an unchanged module keeps its selection.
func (h *DependencyHandler) refreshResolvedModules(
	ctx context.Context,
	current regapi.State,
	transcoder payload.Transcoder,
	resolution *regapi.DependencyResolution,
	desiredDeps []desiredDependency,
) ([]ResolvedModule, error) {
	lockedVersions := storedResolutionVersions(resolution)
	deploymentVersions, err := h.installedModuleVersions(ctx, transcoder, current)
	if err != nil {
		return nil, err
	}
	for module, version := range deploymentVersions {
		lockedVersions[module] = version
	}
	if h.lock != nil {
		for _, mod := range h.lock.GetModules() {
			if mod.Name != "" && mod.Version != "" {
				lockedVersions[mod.Name] = mod.Version
			}
		}
	}
	return h.resolveEffectiveModules(ctx, dependencyDefinitions(desiredDeps), lockedVersions, resolution)
}

// changedDependencyParameterModules returns the modules whose authored root
// parameters differ across a history transition. Dependency parameters belong
// to the referenced component's declared namespace and cannot mutate another
// module, including through a fully qualified requirement ID.
func changedDependencyParameterModules(
	ctx context.Context,
	current regapi.State,
	target regapi.State,
	transcoder payload.Transcoder,
) (map[string]struct{}, error) {
	type declaration struct {
		definition DependencyDefinition
		present    bool
	}

	decodeRoots := func(state regapi.State) (map[string]declaration, error) {
		roots := make(map[string]declaration)
		for _, entry := range state {
			if !isRootDependency(entry) {
				continue
			}
			definition, err := decodeDependency(ctx, transcoder, entry)
			if err != nil {
				return nil, err
			}
			roots[idKey(entry.ID)] = declaration{definition: definition, present: true}
		}
		return roots, nil
	}

	currentRoots, err := decodeRoots(current)
	if err != nil {
		return nil, err
	}
	targetRoots, err := decodeRoots(target)
	if err != nil {
		return nil, err
	}

	changed := make(map[string]struct{})
	rootIDs := make(map[string]struct{}, len(currentRoots)+len(targetRoots))
	for id := range currentRoots {
		rootIDs[id] = struct{}{}
	}
	for id := range targetRoots {
		rootIDs[id] = struct{}{}
	}
	for id := range rootIDs {
		before := currentRoots[id]
		after := targetRoots[id]
		if before.present == after.present && reflect.DeepEqual(before.definition.Parameters, after.definition.Parameters) {
			continue
		}
		for _, item := range []declaration{before, after} {
			if !item.present {
				continue
			}
			if item.definition.Component != "" {
				changed[item.definition.Component] = struct{}{}
			}
		}
	}
	return changed, nil
}

// storedVersionSatisfies validates selectors that can be checked without the
// resolver. A label is intentionally accepted here: the graph digest binds the
// authored label to the exact version selected when the graph was created, and
// resolving the moving label again would defeat deterministic offline restore.
func storedVersionSatisfies(version, constraint string) bool {
	if strings.HasPrefix(strings.TrimSpace(constraint), "@") {
		return strings.TrimSpace(version) != ""
	}
	return lockedVersionSatisfies(version, constraint)
}
func selectedModuleVersion(modules []ResolvedModule, component string) (string, bool) {
	for _, mod := range modules {
		if mod.Org+"/"+mod.Name == component {
			return mod.Version, true
		}
	}
	return "", false
}
func dependencyResolution(roots, references []desiredDependency, modules []ResolvedModule) *regapi.DependencyResolution {
	resolved := &regapi.DependencyResolution{
		InputDigest: dependencyInputDigest(roots),
		Roots:       dependencyRoots(roots),
		References:  dependencyReferenceRoots(references),
		Modules:     make([]regapi.ResolvedModule, 0, len(modules)),
	}
	for _, mod := range modules {
		if mod.Org == "" || mod.Name == "" || mod.Version == "" {
			continue
		}
		resolved.Modules = append(resolved.Modules, regapi.ResolvedModule{
			Name:      mod.Org + "/" + mod.Name,
			Version:   mod.Version,
			VersionID: mod.VersionID,
			Source:    mod.Source,
			Digest:    mod.Digest,
			SizeBytes: mod.SizeBytes,
			Protected: mod.Protected,
		})
	}
	return resolved.Canonical()
}

// dependencyReferenceRoots renders folded references for the durable
// resolution; an absent constraint is recorded as the explicit wildcard so
// every stored reference carries a non-empty version.
func dependencyReferenceRoots(references []desiredDependency) []regapi.DependencyRoot {
	result := dependencyRoots(references)
	for i := range result {
		if strings.TrimSpace(result[i].Version) == "" {
			result[i].Version = "*"
		}
	}
	return result
}
func dependencyRoots(roots []desiredDependency) []regapi.DependencyRoot {
	result := make([]regapi.DependencyRoot, 0, len(roots))
	for _, root := range roots {
		result = append(result, regapi.DependencyRoot{
			ID: root.entry.ID.String(), Component: root.definition.Component, Version: root.definition.Version,
		})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].ID < result[j].ID })
	return result
}
func dependencyInputDigest(roots []desiredDependency) string {
	canonical := dependencyRoots(roots)
	data, _ := json.Marshal(canonical)
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}
func (h *DependencyHandler) collectResolutionDependencies(
	ctx context.Context,
	snapshot regapi.State,
	transcoder payload.Transcoder,
	roots []regapi.DependencyRoot,
	references []regapi.DependencyRoot,
) ([]desiredDependency, []desiredDependency, error) {
	byID := make(map[string]regapi.Entry, len(snapshot))
	for _, entry := range snapshot {
		if isRootDependency(entry) {
			byID[idKey(entry.ID)] = entry
		}
	}
	if len(byID) != len(roots)+len(references) {
		return nil, nil, NewStoredResolutionError("stored dependency root set does not match current declarations", map[string]any{
			"stored":  len(roots) + len(references),
			"current": len(byID),
		})
	}
	deps := make([]desiredDependency, 0, len(roots))
	seenIDs := make(map[string]struct{}, len(roots)+len(references))
	seenComponents := make(map[string]string, len(roots))
	for _, root := range roots {
		rootKey := idKey(regapi.ParseID(root.ID))
		if rootKey == ":" {
			return nil, nil, NewStoredResolutionError("stored dependency root has an empty id", nil)
		}
		if _, duplicate := seenIDs[rootKey]; duplicate {
			return nil, nil, NewStoredResolutionError("duplicate stored dependency root", map[string]any{"root_id": root.ID})
		}
		seenIDs[rootKey] = struct{}{}
		entry, ok := byID[rootKey]
		if !ok {
			return nil, nil, NewStoredResolutionError("stored dependency root is missing", map[string]any{"root_id": root.ID})
		}
		definition, err := decodeDependency(ctx, transcoder, entry)
		if err != nil {
			return nil, nil, err
		}
		if definition.Component != root.Component || definition.Version != root.Version {
			return nil, nil, NewStoredResolutionError("stored dependency root does not match its entry", map[string]any{
				"root_id":  root.ID,
				"expected": root.Component + "@" + root.Version,
				"got":      definition.Component + "@" + definition.Version,
			})
		}
		if previousID, duplicate := seenComponents[definition.Component]; duplicate {
			return nil, nil, NewStoredResolutionError("duplicate stored dependency component across roots", map[string]any{
				"component": definition.Component,
				"first":     previousID,
				"second":    root.ID,
			})
		}
		seenComponents[definition.Component] = root.ID
		deps = append(deps, desiredDependency{entry: entry, definition: definition})
	}
	refs := make([]desiredDependency, 0, len(references))
	for _, reference := range references {
		refKey := idKey(regapi.ParseID(reference.ID))
		if refKey == ":" {
			return nil, nil, NewStoredResolutionError("stored dependency reference has an empty id", nil)
		}
		if _, duplicate := seenIDs[refKey]; duplicate {
			return nil, nil, NewStoredResolutionError("duplicate stored dependency declaration", map[string]any{"reference_id": reference.ID})
		}
		seenIDs[refKey] = struct{}{}
		entry, ok := byID[refKey]
		if !ok {
			return nil, nil, NewStoredResolutionError("stored dependency reference is missing", map[string]any{"reference_id": reference.ID})
		}
		definition, err := decodeDependency(ctx, transcoder, entry)
		if err != nil {
			return nil, nil, err
		}
		declared := strings.TrimSpace(definition.Version)
		if declared == "" {
			declared = "*"
		}
		if definition.Component != reference.Component || declared != reference.Version {
			return nil, nil, NewStoredResolutionError("stored dependency reference does not match its entry", map[string]any{
				"reference_id": reference.ID,
				"expected":     reference.Component + "@" + reference.Version,
				"got":          definition.Component + "@" + declared,
			})
		}
		if _, anchored := seenComponents[definition.Component]; !anchored {
			return nil, nil, NewStoredResolutionError("stored dependency reference has no root for its component", map[string]any{
				"reference_id": reference.ID,
				"component":    definition.Component,
			})
		}
		refs = append(refs, desiredDependency{entry: entry, definition: definition})
	}
	return deps, refs, nil
}
func (h *DependencyHandler) collectSnapshotDependencies(
	ctx context.Context,
	snapshot regapi.State,
	transcoder payload.Transcoder,
) ([]desiredDependency, error) {
	deps := make([]desiredDependency, 0)
	for _, entry := range snapshot {
		if !isRootDependency(entry) {
			continue
		}
		def, err := decodeDependency(ctx, transcoder, entry)
		if err != nil {
			return nil, err
		}
		if def.Component == "" {
			return nil, NewDependencyEntryInvalidError(entry.ID.String(), "component is required", "")
		}
		deps = append(deps, desiredDependency{
			entry:      entry,
			definition: def,
		})
	}
	return deps, nil
}
func dependencyDefinitions(deps []desiredDependency) []DependencyDefinition {
	roots := make([]DependencyDefinition, 0, len(deps))
	for _, dep := range deps {
		roots = append(roots, dep.definition)
	}
	return roots
}
func (h *DependencyHandler) collectDesiredDependencies(
	ctx context.Context,
	op regapi.Operation,
	snapshot regapi.State,
	transcoder payload.Transcoder,
	freshRoots map[string]struct{},
) ([]desiredDependency, []desiredDependency, error) {
	deps := make(map[string]desiredDependency)
	operationID := op.Entry.ID

	current, err := h.collectSnapshotDependencies(ctx, snapshot, transcoder)
	if err != nil {
		return nil, nil, err
	}
	for _, dep := range current {
		deps[idKey(dep.entry.ID)] = dep
	}

	switch op.Kind {
	case regapi.EntryDelete:
		delete(deps, idKey(op.Entry.ID))
	case regapi.EntryCreate, regapi.EntryUpdate:
		entry, ok := resolveOperationEntry(op, snapshot)
		if !ok {
			return nil, nil, NewDependencyEntryMissingError(op.Entry.ID.String())
		}
		if !isRootDependency(entry) {
			break
		}
		def, err := decodeDependency(ctx, transcoder, entry)
		if err != nil {
			return nil, nil, err
		}
		deps[idKey(entry.ID)] = desiredDependency{
			entry:      entry,
			definition: def,
		}
	}

	result := make([]desiredDependency, 0, len(deps))
	for id, dep := range deps {
		if dep.definition.Component == "" {
			return nil, nil, NewDependencyEntryInvalidError(id, "component is required", "")
		}
		result = append(result, dep)
	}
	fresh := make(map[string]struct{}, 1)
	if op.Kind == regapi.EntryCreate {
		fresh[idKey(operationID)] = struct{}{}
	}
	for key := range freshRoots {
		fresh[key] = struct{}{}
	}
	return foldRootDependencyComponents(result, fresh, true)
}

// foldRootDependencyComponents partitions root declarations into one
// controlling root per component plus folded references, independent of any
// operation: a declaration carrying parameters controls (several carriers must
// agree), ties break on the lowest canonical entry key, and — in the live path
// — a declaration introduced by the current changeset (`fresh`) never controls
// while an established one exists. The choice is therefore reconstructible
// from the declarations alone and stable across later evaluations.
//
// strict mode (live expansion) conflicts on parameter disagreement and on a
// fresh parameter-carrying duplicate that would have to seize control.
// Lenient mode (reconciliation of committed state) never conflicts: replay is
// anchored by the stored root/reference partition, and parameter drift is
// handled by the parameter reconciliation sweep, so a disagreement must not
// wedge boot.
//
// Folded reference constraints are normalized here (trimmed, absent becomes
// the explicit wildcard) so the durable record, the solver input, and strict
// replay all see one spelling.
func foldRootDependencyComponents(deps []desiredDependency, fresh map[string]struct{}, strict bool) (roots, references []desiredDependency, err error) {
	sort.SliceStable(deps, func(i, j int) bool {
		return idKey(deps[i].entry.ID) < idKey(deps[j].entry.ID)
	})

	groups := make(map[string][]desiredDependency, len(deps))
	order := make([]string, 0, len(deps))
	for _, dep := range deps {
		component := dep.definition.Component
		if _, seen := groups[component]; !seen {
			order = append(order, component)
		}
		groups[component] = append(groups[component], dep)
	}

	isFresh := func(dep desiredDependency) bool {
		if len(fresh) == 0 {
			return false
		}
		_, ok := fresh[idKey(dep.entry.ID)]
		return ok
	}
	hasParams := func(dep desiredDependency) bool { return len(dep.definition.Parameters) > 0 }

	roots = make([]desiredDependency, 0, len(deps))
	for _, component := range order {
		group := groups[component]

		// Election: parameter carriers first, established before fresh, then
		// the lowest canonical key. The input is already key-sorted, so the
		// first matching declaration wins deterministically.
		pick := func(accept func(desiredDependency) bool) (desiredDependency, bool) {
			for _, dep := range group {
				if accept(dep) {
					return dep, true
				}
			}
			return desiredDependency{}, false
		}
		controller, elected := pick(func(d desiredDependency) bool { return hasParams(d) && !isFresh(d) })
		if !elected {
			controller, elected = pick(func(d desiredDependency) bool { return !isFresh(d) })
		}
		if !elected {
			controller, elected = pick(hasParams)
		}
		if !elected {
			controller = group[0]
		}
		if strict && controller.definition.Version == "" {
			return nil, nil, apierror.New(apierror.Invalid, fmt.Sprintf(
				"dependency %s (%s) requires a version; specify a version constraint or explicit '*'",
				controller.entry.ID.String(), component,
			)).WithDetails(attrs.NewBagFrom(map[string]any{
				"entry_id":  controller.entry.ID.String(),
				"component": component,
				"field":     "version",
			}))
		}

		for _, dep := range group {
			if idsEqual(dep.entry.ID, controller.entry.ID) {
				// The controlling declaration itself, possibly observed through
				// two ID spellings; every such shape stays a root.
				roots = append(roots, dep)
				continue
			}
			if strict && hasParams(dep) && !dependencyParametersEqual(dep.definition.Parameters, controller.definition.Parameters) {
				return nil, nil, NewDependencyRootConflictError(component, controller.entry.ID.String(), dep.entry.ID.String())
			}
			reference := dep
			reference.definition.Version = strings.TrimSpace(reference.definition.Version)
			if reference.definition.Version == "" {
				reference.definition.Version = "*"
			}
			references = append(references, reference)
		}
	}
	return roots, references, nil
}

// dependencyParametersEqual compares parameter sets by name over their
// canonical JSON forms, so transcoder-specific value typing cannot split
// equal declarations.
func dependencyParametersEqual(a, b []Parameter) bool {
	if len(a) != len(b) {
		return false
	}
	canonical := func(params []Parameter) string {
		pairs := make([]string, 0, len(params))
		for _, p := range params {
			value, err := json.Marshal(p.Value)
			if err != nil {
				value = []byte(fmt.Sprintf("%#v", p.Value))
			}
			pairs = append(pairs, p.Name+"="+string(value))
		}
		sort.Strings(pairs)
		return strings.Join(pairs, "\x00")
	}
	return canonical(a) == canonical(b)
}
func (h *DependencyHandler) installedModuleVersions(ctx context.Context, transcoder payload.Transcoder, snapshot regapi.State) (map[string]string, error) {
	versions, _ := h.currentModuleIdentities(ctx)
	if h.lock == nil {
		return versions, nil
	}
	installedRoots, err := rootDependencyModules(ctx, transcoder, snapshot)
	if err != nil {
		return nil, err
	}
	for _, mod := range h.lock.GetModules() {
		if mod.Name == "" || mod.Version == "" {
			continue
		}
		if _, installed := installedRoots[mod.Name]; !installed {
			continue
		}
		if _, ok := versions[mod.Name]; !ok {
			versions[mod.Name] = mod.Version
		}
	}
	return versions, nil
}
func mergeLinkDependencies(explicitDeps, moduleEntries []regapi.Entry) []regapi.Entry {
	merged := make([]regapi.Entry, 0, len(explicitDeps)+len(moduleEntries))
	seen := make(map[string]struct{}, len(explicitDeps)+len(moduleEntries))

	appendDep := func(entry regapi.Entry) {
		if entry.Kind != regapi.NamespaceDependency {
			return
		}
		key := idKey(entry.ID)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		merged = append(merged, entry)
	}

	for _, entry := range explicitDeps {
		appendDep(entry)
	}
	for _, entry := range moduleEntries {
		appendDep(entry)
	}

	return merged
}
