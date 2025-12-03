package stages

import (
	"context"
	"fmt"
	"strings"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
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

type linkStage struct{}

// Link creates a new linking stage that resolves requirements to their values
// and applies them to target entries.
func Link() boot.Stage {
	return &linkStage{}
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
		if e.Kind != registry.KindNamespaceRequirement {
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

	// Collect and decode dependencies
	dependencies := make(map[string]decodedDependency)
	for _, e := range *entries {
		if e.Kind != registry.KindNamespaceDependency {
			continue
		}

		def, err := entry.DecodeEntryConfig[DependencyDefinition](ctx, transcoder, e)
		if err != nil {
			return NewDecodeDependencyError(e.ID.String(), err)
		}

		dependencies[e.ID.String()] = decodedDependency{
			entry:      e,
			definition: def,
		}
	}

	// Process each requirement, log warnings instead of failing
	warningCount := 0
	for _, req := range requirements {
		if err := s.processRequirement(req, dependencies, entries, mutator); err != nil {
			log.Warn("unresolved requirement",
				zap.String("requirement", req.entry.ID.String()),
				zap.Error(err))
			warningCount++
		}
	}

	if warningCount > 0 {
		log.Info("link stage completed with warnings",
			zap.Int("warnings", warningCount),
			zap.Int("total_requirements", len(requirements)))
	}

	return nil
}

type decodedRequirement struct {
	entry      registry.Entry
	definition *RequirementDefinition
}

type decodedDependency struct {
	entry      registry.Entry
	definition *DependencyDefinition
}

func (s *linkStage) processRequirement(
	req decodedRequirement,
	dependencies map[string]decodedDependency,
	entries *[]registry.Entry,
	mutator *entry.Mutator,
) error {
	requirementName := req.entry.ID.Name

	// Find parameter value from dependencies
	value, err := s.resolveValue(requirementName, req.definition.Default, dependencies)
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
	dependencies map[string]decodedDependency,
) (string, error) {
	// Find all dependencies that have a parameter with this name
	var foundValues []struct {
		value string
		depID string
	}

	for _, dep := range dependencies {
		for _, param := range dep.definition.Parameters {
			if param.Name == requirementName {
				foundValues = append(foundValues, struct {
					value string
					depID string
				}{
					value: param.Value,
					depID: dep.entry.ID.String(),
				})
			}
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
		entry := &(*entries)[i]

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
				if entry.ID.NS == targetNS && entry.ID.Name == targetName {
					results = append(results, entry)
				}
			}
			continue
		}

		// Local namespace reference (just name)
		if entry.ID.NS == requirementNS && entry.ID.Name == targetEntry {
			results = append(results, entry)
		}
	}

	return results
}
