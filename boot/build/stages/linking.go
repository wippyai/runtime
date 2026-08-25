// SPDX-License-Identifier: MPL-2.0

package stages

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/system/entry"
	"go.uber.org/zap"
)

// RequirementDefinition represents the data structure of an ns.requirement entry.
// Default carries the value verbatim in its source type (int, bool, float,
// string, ...) so typed requirements flow into their targets unchanged. A
// present default — including an empty string "" — makes the requirement
// optional and resolves to that value when no dependency parameter supplies one.
// A nil Default (an absent default key or an explicit null) leaves the
// requirement mandatory and unresolved.
type RequirementDefinition struct {
	Default any                 `json:"default" yaml:"default"`
	Targets []RequirementTarget `json:"targets" yaml:"targets"`
}

// RequirementTarget represents a single target in a requirement definition
type RequirementTarget struct {
	Entry string `json:"entry" yaml:"entry"`
	Path  string `json:"path" yaml:"path"`
}

// DependencyDefinition represents the data structure of an ns.dependency entry
type DependencyDefinition struct {
	Component  string      `json:"component" yaml:"component"`
	Version    string      `json:"version" yaml:"version"`
	Parameters []Parameter `json:"parameters" yaml:"parameters"`
}

// Parameter represents a single parameter in a dependency definition.
// Value carries the supplied value in its source type so typed parameters flow
// into requirement targets unchanged.
type Parameter struct {
	Value any    `json:"value" yaml:"value"`
	Name  string `json:"name" yaml:"name"`
}

type LinkOption func(*linkStage)

type linkStage struct {
	strictModules     map[string]struct{}
	provenance        registry.ProvMap
	dependencyEntries []registry.Entry
	explicitDeps      bool
	strict            bool
	strictModuleScope bool
}

// Link creates a new linking stage that resolves requirements to their values
// and applies them to target entries.
//
// prov is the provenance of the entries being linked and is the only source of
// module membership: which module declares a namespace, which dependency is a
// package-injected transitive, and which requirement falls under strict module
// scope. It is a required argument so every caller answers that question; a
// build of a single source tree, which has no module world, passes nil.
func Link(prov registry.ProvMap, opts ...LinkOption) boot.Stage {
	stage := &linkStage{provenance: prov}
	for _, opt := range opts {
		if opt != nil {
			opt(stage)
		}
	}
	return stage
}

// WithDependencies provides explicit dependency entries for requirement resolution.
// When set, Link will use these entries instead of scanning the entry list.
func WithDependencies(entries []registry.Entry) LinkOption {
	return func(s *linkStage) {
		s.dependencyEntries = entries
		s.explicitDeps = true
	}
}

// WithStrictRequirements makes unresolved requirements fail the link stage.
// The default remains warning-only for source builds, where optional or
// environment-specific requirements may be intentionally unresolved.
func WithStrictRequirements() LinkOption {
	return func(s *linkStage) {
		s.strict = true
	}
}

// WithStrictRequirementModules makes unresolved requirements fail only when
// they belong to one of the provided module identities. This keeps dependency
// installs strict for the modules being expanded without turning unrelated
// application requirements into install blockers.
func WithStrictRequirementModules(modules []string) LinkOption {
	return func(s *linkStage) {
		s.strict = true
		s.strictModuleScope = true
		s.strictModules = make(map[string]struct{}, len(modules))
		for _, module := range modules {
			if strings.TrimSpace(module) != "" {
				s.strictModules[module] = struct{}{}
			}
		}
	}
}

func (s *linkStage) Name() string {
	return "link"
}

func (s *linkStage) Execute(ctx context.Context, entries *[]registry.Entry) error {
	transcoder := payload.GetTranscoder(ctx)
	if transcoder == nil {
		return ErrTranscoderNotFound
	}

	log := logs.GetLogger(ctx)
	mutator := entry.NewMutator(transcoder)

	// Collect and decode requirements
	requirements := make(map[string]decodedRequirement)
	for _, e := range *entries {
		if e.Kind != registry.NamespaceRequirement {
			continue
		}

		def, err := entry.DecodeEntryConfigRaw[RequirementDefinition](ctx, transcoder, e)
		if err != nil {
			return NewDecodeRequirementError(e.ID.String(), err)
		}

		requirements[e.ID.String()] = decodedRequirement{
			entry:      e,
			definition: def,
		}
	}

	moduleNamespaces, err := s.declaredModuleNamespaces(*entries)
	if err != nil {
		return err
	}

	dependencies, err := s.collectDependencies(ctx, transcoder, entries, moduleNamespaces)
	if err != nil {
		return err
	}

	// Normalize every dependency parameter to the set of concrete requirement
	// ids it addresses up front. A bare name fans out to all owned requirements
	// of that name; value conflicts on a concrete requirement fail later.
	bindings, err := normalizeBindings(requirements, dependencies)
	if err != nil {
		return err
	}

	// Process each requirement in sorted id order so applied values, warnings
	// and strict errors are deterministic regardless of entry ordering.
	warningCount := 0
	var unresolved []error
	for _, id := range sortedKeys(requirements) {
		req := requirements[id]
		if err := s.processRequirement(req, bindings[id], entries, mutator); err != nil {
			log.Warn("unresolved requirement",
				zap.String("requirement", req.entry.ID.String()),
				zap.Error(err))
			warningCount++
			if s.shouldFailUnresolvedRequirement(req) {
				unresolved = append(unresolved, err)
			}
		}
	}

	if warningCount > 0 {
		log.Info("link stage completed with warnings",
			zap.Int("warnings", warningCount),
			zap.Int("total_requirements", len(requirements)))
	}

	if s.strict && len(unresolved) > 0 {
		return NewUnresolvedRequirementsError(unresolved)
	}

	return nil
}

func (s *linkStage) shouldFailUnresolvedRequirement(req decodedRequirement) bool {
	if !s.strict {
		return false
	}
	if s.strictModuleScope {
		_, ok := s.strictModules[s.entryModule(req.entry)]
		return ok
	}
	return true
}

func (s *linkStage) collectDependencies(
	ctx context.Context,
	transcoder payload.Transcoder,
	entries *[]registry.Entry,
	moduleNamespaces map[string]string,
) (map[string]decodedDependency, error) {
	source := *entries
	if s.explicitDeps {
		source = s.dependencyEntries
	}

	dependencies := make(map[string]decodedDependency)
	for _, e := range source {
		if e.Kind != registry.NamespaceDependency {
			continue
		}

		def, err := entry.DecodeEntryConfigRaw[DependencyDefinition](ctx, transcoder, e)
		if err != nil {
			return nil, NewDecodeDependencyError(e.ID.String(), err)
		}
		ownedNamespace := moduleNamespaces[def.Component]
		if ownedNamespace == "" && len(def.Parameters) > 0 && s.loadedModule(*entries, def.Component) {
			return nil, fmt.Errorf(
				"dependency %s cannot bind parameters for component %s: loaded module has no ns.definition root namespace",
				e.ID.String(),
				def.Component,
			)
		}

		dependencies[e.ID.String()] = decodedDependency{
			definition:     def,
			ownedNamespace: ownedNamespace,
			transitive:     s.transitiveDependency(e),
		}
	}

	return dependencies, nil
}

type decodedRequirement struct {
	definition *RequirementDefinition
	entry      registry.Entry
}

type decodedDependency struct {
	definition     *DependencyDefinition
	ownedNamespace string
	// transitive marks a dependency injected by a package, as opposed to an
	// explicitly selected deployment root. Ownership and root provenance are
	// independent for published application modules.
	transitive bool
}

// owns reports whether a dependency addresses a requirement. A module owns its
// declared ns.definition namespace and its children. Package names never
// participate in registry namespace ownership.
func (d decodedDependency) owns(req decodedRequirement) bool {
	return req.entry.ID.NS == d.ownedNamespace || strings.HasPrefix(req.entry.ID.NS, d.ownedNamespace+".")
}

// binding records one dependency parameter resolved to a single concrete
// requirement id. It exists only in memory; fully-qualified ids are never
// written back into ns.dependency entries. transitive carries the provenance of
// the dependency entry the parameter came from (see decodedDependency).
type binding struct {
	value         any
	dependencyID  string
	requirementID string
	originalName  string
	transitive    bool
}

// normalizeBindings maps every dependency parameter to the set of concrete
// requirement ids it addresses, grouped by requirement id. A bare parameter
// fans out through the dependency's owned addressing index to every owned
// requirement of that bare name; a full ns:name parameter addresses that exact
// requirement only when it belongs to the referenced module. A parameter
// addressing nothing is dropped. Iteration is sorted so the result is
// independent of entry ordering.
func normalizeBindings(
	requirements map[string]decodedRequirement,
	dependencies map[string]decodedDependency,
) (map[string][]binding, error) {
	reqIDs := sortedKeys(requirements)
	bindings := make(map[string][]binding)

	for _, depID := range sortedKeys(dependencies) {
		dep := dependencies[depID]
		owned := ownedAddressIndex(dep, reqIDs, requirements)

		for _, param := range dep.definition.Parameters {
			for _, reqID := range resolveParameter(param.Name, requirements, owned) {
				bindings[reqID] = append(bindings[reqID], binding{
					value:         param.Value,
					dependencyID:  depID,
					requirementID: reqID,
					originalName:  param.Name,
					transitive:    dep.transitive,
				})
			}
		}
	}

	for reqID := range bindings {
		list := bindings[reqID]
		sort.Slice(list, func(i, j int) bool {
			if list[i].dependencyID != list[j].dependencyID {
				return list[i].dependencyID < list[j].dependencyID
			}
			return list[i].originalName < list[j].originalName
		})
	}

	return bindings, nil
}

// ownedAddressIndex maps each address key a dependency accepts to the concrete
// requirement ids it fans out to. Every owned requirement registers under both
// its bare name and its canonical registry id. Two or more owned
// requirements sharing one bare name is the fan-out set: a bare parameter of
// that name feeds its value to all of them. A given requirement id appears at
// most once per key.
func ownedAddressIndex(
	dep decodedDependency,
	reqIDs []string,
	requirements map[string]decodedRequirement,
) map[string][]string {
	owned := make(map[string][]string)
	for _, reqID := range reqIDs {
		req := requirements[reqID]
		if !dep.owns(req) {
			continue
		}
		keys := []string{
			req.entry.ID.Name,
			reqID,
		}
		for _, key := range keys {
			if !containsID(owned[key], reqID) {
				owned[key] = append(owned[key], reqID)
			}
		}
	}
	return owned
}

// resolveParameter maps a single parameter name to the set of concrete
// requirement ids it addresses. A canonical ns:name is already a complete
// registry address and selects that exact requirement. A bare name uses the
// dependency component's declared namespace index. Package identities are
// never converted into registry namespaces. A name that addresses nothing
// returns an empty set.
func resolveParameter(
	name string,
	requirements map[string]decodedRequirement,
	owned map[string][]string,
) []string {
	if strings.Contains(name, ":") {
		if _, exists := requirements[name]; exists {
			return []string{name}
		}
		return nil
	}
	return owned[name]
}

func containsID(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func (s *linkStage) processRequirement(
	req decodedRequirement,
	bindings []binding,
	entries *[]registry.Entry,
	mutator *entry.Mutator,
) error {
	value, err := resolveValue(req, bindings)
	if err != nil {
		return NewRequirementError(req.entry.ID.Name, req.entry.ID.NS, err)
	}

	// Validate targets exist
	if len(req.definition.Targets) == 0 {
		return NewNoTargetsError(req.entry.ID.String())
	}

	// Apply value to each target
	for _, target := range req.definition.Targets {
		if err := s.applyTarget(target, value, req.entry.ID.NS, entries, mutator); err != nil {
			return NewRequirementTargetError(req.entry.ID.String(), target.Entry, target.Path, err)
		}
	}

	return nil
}

// rootOverTransitive drops transitive bindings when any root binding is present,
// leaving order unchanged. With no root binding it returns the input unchanged.
func rootOverTransitive(bindings []binding) []binding {
	hasRoot := false
	for _, b := range bindings {
		if !b.transitive {
			hasRoot = true
			break
		}
	}
	if !hasRoot {
		return bindings
	}
	roots := make([]binding, 0, len(bindings))
	for _, b := range bindings {
		if !b.transitive {
			roots = append(roots, b)
		}
	}
	return roots
}

// resolveValue selects the value for a requirement from its bindings, falling
// back to the definition default. Explicit root dependency parameters override
// transitive ones for the same concrete requirement id; among the surviving
// same-provenance bindings, values that disagree conflict.
func resolveValue(req decodedRequirement, bindings []binding) (any, error) {
	effective := rootOverTransitive(bindings)

	if len(effective) > 0 {
		first := effective[0].value
		for _, b := range effective[1:] {
			if !reflect.DeepEqual(b.value, first) {
				conflicts := make([]string, 0, len(effective))
				for _, b := range effective {
					conflicts = append(conflicts, fmt.Sprintf("%s=%v (from %s)", req.entry.ID.Name, b.value, b.dependencyID))
				}
				return nil, NewParameterConflictError(strings.Join(conflicts, ", "))
			}
		}
		return effective[0].value, nil
	}

	// Fall back to the default when one is defined. A present-but-empty
	// default ("") is a valid resolved value; only an absent default (nil)
	// leaves the requirement unresolved.
	if req.definition.Default != nil {
		return req.definition.Default, nil
	}

	// No value available
	return nil, ErrNoValueAvailable
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func (s *linkStage) applyTarget(
	target RequirementTarget,
	value any,
	requirementNS string,
	entries *[]registry.Entry,
	mutator *entry.Mutator,
) error {
	// Find target entries
	targetEntries := s.findTargetEntries(target.Entry, requirementNS, entries)
	if len(targetEntries) == 0 {
		return ErrNoMatchingEntries
	}

	// Parse path for append operator
	path := strings.TrimSpace(target.Path)
	isAppend := strings.HasSuffix(path, "+=")
	if isAppend {
		path = strings.TrimSpace(strings.TrimSuffix(path, "+="))
	}

	// Apply to each target entry
	for _, targetEntry := range targetEntries {
		if isAppend {
			if err := mutator.Append(targetEntry, path, value); err != nil {
				return NewAppendToEntryError(targetEntry.ID.String(), err)
			}
		} else {
			if err := mutator.Set(targetEntry, path, value); err != nil {
				return NewSetValueInEntryError(targetEntry.ID.String(), err)
			}
		}
	}

	return nil
}

func (s *linkStage) findTargetEntries(
	targetEntry string,
	requirementNS string,
	entries *[]registry.Entry,
) []*registry.Entry {
	var results []*registry.Entry

	for i := range *entries {
		e := &(*entries)[i]

		// Empty entry is not supported
		if targetEntry == "" {
			continue
		}

		// Check for cross-namespace reference (ns:name)
		if strings.Contains(targetEntry, ":") {
			parts := strings.SplitN(targetEntry, ":", 2)
			if len(parts) == 2 {
				targetNS := parts[0]
				targetName := parts[1]
				if e.ID.NS == targetNS && e.ID.Name == targetName {
					results = append(results, e)
				}
			}
			continue
		}

		// Local namespace reference (just name)
		if e.ID.NS == requirementNS && e.ID.Name == targetEntry {
			results = append(results, e)
		}
	}

	return results
}

// entryModule returns the module owning an entry, empty when the entry is
// host-authored or the build carries no module world.
func (s *linkStage) entryModule(entry registry.Entry) string {
	return s.provenance[entry.ID].Module
}

// transitiveDependency reports a declaration a package injected, as opposed to
// one the deployment selected. Ownership and root selection are independent.
func (s *linkStage) transitiveDependency(entry registry.Entry) bool {
	record := s.provenance[entry.ID]
	return record.Module != "" && !record.Root
}

func (s *linkStage) loadedModule(entries []registry.Entry, module string) bool {
	for _, entry := range entries {
		if s.entryModule(entry) == module {
			return true
		}
	}
	return false
}

// declaredModuleNamespaces returns the canonical registry namespace exported
// by each loaded module. Publishing requires exactly one ns.definition per
// module, and provenance records which module produced that entry. This is
// the authoritative bridge between a Hub component name (org/module) and its
// registry namespace; neither spelling nor pluralization is inferred.
func (s *linkStage) declaredModuleNamespaces(entries []registry.Entry) (map[string]string, error) {
	namespaces := make(map[string]string)
	owners := make(map[string]string)
	for _, entry := range entries {
		if entry.Kind != registry.NamespaceDefinition {
			continue
		}
		module := s.entryModule(entry)
		if module == "" {
			continue
		}
		namespace := strings.TrimSpace(entry.ID.NS)
		if namespace == "" {
			continue
		}
		if existing := namespaces[module]; existing != "" && existing != namespace {
			return nil, fmt.Errorf(
				"module %s declares multiple namespaces: %s and %s",
				module,
				existing,
				namespace,
			)
		}
		if owner := owners[namespace]; owner != "" && owner != module {
			return nil, fmt.Errorf(
				"namespace %s is declared by multiple modules: %s and %s",
				namespace,
				owner,
				module,
			)
		}
		namespaces[module] = namespace
		owners[namespace] = module
	}
	return namespaces, nil
}
