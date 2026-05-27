// SPDX-License-Identifier: MPL-2.0

package stages

import (
	"context"
	"fmt"
	"strings"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/boot/deps/graph"
	"github.com/wippyai/runtime/internal/entry"
	"go.uber.org/zap"
)

// RequirementDefinition represents the data structure of an ns.requirement entry
type RequirementDefinition struct {
	Default string              `json:"default" yaml:"default"`
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

// Parameter represents a single parameter in a dependency definition
type Parameter struct {
	Name  string `json:"name" yaml:"name"`
	Value string `json:"value" yaml:"value"`
}

type LinkOption func(*linkStage)

type linkStage struct {
	strictModules     map[string]struct{}
	dependencyEntries []registry.Entry
	explicitDeps      bool
	strict            bool
	strictModuleScope bool
}

// Link creates a new linking stage that resolves requirements to their values
// and applies them to target entries.
func Link(opts ...LinkOption) boot.Stage {
	stage := &linkStage{}
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

		def, err := entry.DecodeEntryConfig[RequirementDefinition](ctx, transcoder, e)
		if err != nil {
			return NewDecodeRequirementError(e.ID.String(), err)
		}

		requirements[e.ID.String()] = decodedRequirement{
			entry:      e,
			definition: def,
		}
	}

	dependencies, err := s.collectDependencies(ctx, transcoder, entries)
	if err != nil {
		return err
	}

	// Process each requirement, log warnings instead of failing
	warningCount := 0
	var unresolved []error
	for _, req := range requirements {
		if err := s.processRequirement(req, dependencies, entries, mutator); err != nil {
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
		module := requirementModuleFromEntry(req.entry)
		_, ok := s.strictModules[module]
		return ok
	}
	return true
}

func (s *linkStage) collectDependencies(ctx context.Context, transcoder payload.Transcoder, entries *[]registry.Entry) (map[string]decodedDependency, error) {
	source := *entries
	if s.explicitDeps {
		source = s.dependencyEntries
	}

	dependencies := make(map[string]decodedDependency)
	for _, e := range source {
		if e.Kind != registry.NamespaceDependency {
			continue
		}

		def, err := entry.DecodeEntryConfig[DependencyDefinition](ctx, transcoder, e)
		if err != nil {
			return nil, NewDecodeDependencyError(e.ID.String(), err)
		}
		moduleNamespace, err := componentToNamespace(def.Component)
		if err != nil {
			return nil, NewInvalidDependencyComponentError(e.ID.String(), def.Component, err)
		}

		dependencies[e.ID.String()] = decodedDependency{
			entry:           e,
			definition:      def,
			moduleNamespace: moduleNamespace,
			component:       def.Component,
		}
	}

	return dependencies, nil
}

type decodedRequirement struct {
	definition *RequirementDefinition
	entry      registry.Entry
}

type decodedDependency struct {
	definition      *DependencyDefinition
	entry           registry.Entry
	moduleNamespace string
	component       string
}

func (s *linkStage) processRequirement(
	req decodedRequirement,
	dependencies map[string]decodedDependency,
	entries *[]registry.Entry,
	mutator *entry.Mutator,
) error {
	requirementName := req.entry.ID.Name
	requirementModule := requirementModuleFromEntry(req.entry)

	// Find parameter value from dependencies
	value, err := s.resolveValue(requirementName, req.definition.Default, req.entry.ID.NS, requirementModule, dependencies)
	if err != nil {
		return NewRequirementError(requirementName, req.entry.ID.NS, err)
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

func (s *linkStage) resolveValue(
	requirementName string,
	defaultValue string,
	requirementNS string,
	requirementModule string,
	dependencies map[string]decodedDependency,
) (string, error) {
	// Find all dependencies that have a parameter with this name
	var foundValues []struct {
		value string
		depID string
	}

	requirementID := requirementNS + ":" + requirementName

	for _, dep := range dependencies {
		for _, param := range dep.definition.Parameters {
			if !matchesRequirement(
				param.Name,
				dep.moduleNamespace,
				dep.component,
				requirementNS,
				requirementName,
				requirementID,
				requirementModule,
			) {
				continue
			}
			foundValues = append(foundValues, struct {
				value string
				depID string
			}{
				value: param.Value,
				depID: dep.entry.ID.String(),
			})
		}
	}

	// Check for conflicts
	if len(foundValues) > 1 {
		// Check if all values are the same
		firstValue := foundValues[0].value
		hasConflict := false
		for _, fv := range foundValues[1:] {
			if fv.value != firstValue {
				hasConflict = true
				break
			}
		}

		if hasConflict {
			var conflicts []string
			for _, fv := range foundValues {
				conflicts = append(conflicts, fmt.Sprintf("%s=%s (from %s)", requirementName, fv.value, fv.depID))
			}
			return "", NewParameterConflictError(strings.Join(conflicts, ", "))
		}
	}

	// Use dependency parameter if found
	if len(foundValues) > 0 {
		return foundValues[0].value, nil
	}

	// Fall back to default
	if defaultValue != "" {
		return defaultValue, nil
	}

	// No value available
	return "", ErrNoValueAvailable
}

func (s *linkStage) applyTarget(
	target RequirementTarget,
	value string,
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

// matchesRequirement checks if a parameter name references a requirement.
// Supports two conventions:
//   - Full ID: "ns:name" matches directly against the requirement entry ID
//   - Bare name: "name" matches either:
//   - requirement name within the computed module namespace, or
//   - requirement name within the same module identity via requirement meta.module
func matchesRequirement(paramName, moduleNS, component, reqNS, reqName, reqID, reqModule string) bool {
	if strings.Contains(paramName, ":") {
		paramNS, paramReqName, ok := strings.Cut(paramName, ":")
		if !ok || paramReqName != reqName {
			return false
		}
		if paramName == reqID {
			return true
		}
		if paramNS != moduleNS {
			return false
		}
		if reqNS == moduleNS {
			return true
		}
		return component != "" && reqModule != "" && component == reqModule
	}

	if paramName != reqName {
		return false
	}

	if moduleNS == reqNS {
		return true
	}

	// Fallback for modules that publish requirements under a different namespace.
	return component != "" && reqModule != "" && component == reqModule
}

func requirementModuleFromEntry(entry registry.Entry) string {
	if entry.Meta == nil {
		return ""
	}
	if module, ok := entry.Meta["module"].(string); ok {
		return module
	}
	return ""
}

func componentToNamespace(component string) (string, error) {
	name, err := graph.ParseName(component)
	if err != nil {
		return "", err
	}
	return name.Organization + "." + name.Module, nil
}
