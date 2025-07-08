package requirementresolver

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/itchyny/gojq"
	"github.com/ponyruntime/pony/api/registry"
	"go.uber.org/zap"
)

// Resolver handles module Definition resolution
type Resolver struct {
	logger *zap.Logger
}

// NewResolver creates a new Resolver instance with the given logger
func NewResolver(logger *zap.Logger) *Resolver {
	return &Resolver{
		logger: logger,
	}
}

// ResolveModuleDefinitions resolves module Definitions and injects parameters
func (r *Resolver) ResolveModuleDefinitions(entries []registry.Entry) error {
	nsRequirements := make(map[string]registry.Entry)
	nsDependencies := make(map[string]registry.Entry)
	nsDefinitions := make(map[string]registry.Entry)

	for _, entry := range entries {
		switch entry.Kind {
		case registry.KindNamespaceDefinition:
			nsDefinitions[entry.ID.Name] = entry
		case registry.KindNamespaceDependency:
			nsDependencies[entry.ID.Name] = entry
		case registry.KindNamespaceRequirement:
			nsRequirements[entry.ID.Name] = entry
		}
	}

	for _, nsDefinition := range nsDefinitions {
		nsDependency, path, err := findDefinitionDependency(nsDefinition, nsDependencies)
		if err != nil {
			r.logger.Warn("failed to find definition dependency",
				zap.String("definition", nsDefinition.ID.Name),
				zap.Error(err))
			continue
		}

		value, err := getValueFromEntry(nsDependency, path)
		if err != nil {
			r.logger.Warn("failed to get value from entry",
				zap.String("dependency", nsDependency.ID.Name),
				zap.String("path", path),
				zap.Error(err))
		}

		nsRequirement, err := findDefinitionRequirement(nsDefinition, nsRequirements)
		if err != nil {
			r.logger.Warn("failed to find Definition Requirement",
				zap.String("Definition", nsDefinition.ID.Name),
				zap.Error(err))
			continue
		}

		requirementTargets, err := getRequirementTargets(nsRequirement)
		if err != nil {
			r.logger.Warn("failed to get requirement targets",
				zap.String("requirement", nsRequirement.ID.Name),
				zap.Error(err))
			continue
		}

		for _, requirementTarget := range requirementTargets {
			targetEntries := findRequirementTargetEntries(requirementTarget, nsRequirement.ID.NS, entries)
			r.applyPathValueToEntriesWithGojq(requirementTarget.Path, value, targetEntries)
		}
	}

	return nil
}

// applyPathValueToEntriesWithGojq applies a value to entries using jq DSL queries
// Now works with raw JSON for the entire entry
func applyPathValueToEntriesWithGojq(jqQuery string, value string, entries []registry.Entry) error {
	errs := make([]error, 0)
	for i := range entries {
		entry := &entries[i]

		// Handle special cases for backward compatibility
		if jqQuery == "kind" {
			entry.Kind = value
			continue
		}

		// don't allow to modify the Data field
		// data := entry.Data.Data()
		// entry.Data = nil

		// Convert entry to raw JSON map
		entryMap, err := entryToRawJSONMap(entry)
		if err != nil {
			errs = append(errs, fmt.Errorf("failed to convert entry to JSON map for entry %s: %w", entry.ID.Name, err))
			continue
		}

		// Apply jq query to the raw JSON map
		updatedMap, err := setValueWithGojqReturnMap(entryMap, jqQuery, value)
		if err != nil {
			errs = append(errs, fmt.Errorf("failed to set value with jq query '%s' for entry %s: %w", jqQuery, entry.ID.Name, err))
			continue
		}

		// Convert the modified map back to registry.Entry
		err = updateEntryFromRawJSONMap(entry, updatedMap)
		if err != nil {
			errs = append(errs, fmt.Errorf("failed to update entry from JSON map for entry %s: %w", entry.ID.Name, err))
		}
	}
	if len(errs) > 0 {
		return joinErrors(errs)
	}
	return nil
}

// entryToRawJSONMap converts a registry.Entry to map[string]interface{} via JSON marshal/unmarshal
func entryToRawJSONMap(entry *registry.Entry) (map[string]interface{}, error) {
	// Temporarily save the Data field and set it to nil to avoid interface serialization issues
	originalData := entry.Data
	entry.Data = nil

	// Use default JSON marshal for the rest of the struct
	b, err := json.Marshal(entry)
	if err != nil {
		// Restore the original Data field before returning error
		entry.Data = originalData
		return nil, err
	}

	// Restore the original Data field
	entry.Data = originalData

	// Unmarshal to map
	var m map[string]interface{}
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}

	return m, nil
}

// updateEntryFromRawJSONMap updates a registry.Entry from a map[string]interface{} via JSON marshal/unmarshal
func updateEntryFromRawJSONMap(entry *registry.Entry, m map[string]interface{}) error {
	// Temporarily save the Data field and set it to nil to avoid interface serialization issues
	originalData := entry.Data
	entry.Data = nil

	// Marshal the map to JSON
	b, err := json.Marshal(m)
	if err != nil {
		// Restore the original Data field before returning error
		entry.Data = originalData
		return err
	}

	// Use default JSON unmarshal for the rest of the struct
	err = json.Unmarshal(b, entry)
	if err != nil {
		// Restore the original Data field before returning error
		entry.Data = originalData
		return err
	}

	// Restore the original Data field
	entry.Data = originalData

	return nil
}

// setValueWithGojqReturnMap is like setValueWithGojq but returns the updated map
func setValueWithGojqReturnMap(data interface{}, path string, value string) (map[string]interface{}, error) {
	trimmedPath := strings.TrimSpace(path)

	var jqQuery string
	if strings.Contains(trimmedPath, "+=") {
		jqQuery = fmt.Sprintf("%s [\"%s\"]", trimmedPath, value)
	} else {
		// For simple assignment, check if the path already has an assignment operator
		if strings.Contains(trimmedPath, "=") {
			// Path already has assignment operator, just add the value
			jqQuery = fmt.Sprintf("%s \"%s\"", trimmedPath, value)
		} else {
			// Path doesn't have assignment operator, add it
			jqQuery = fmt.Sprintf("%s = \"%s\"", trimmedPath, value)
		}
	}

	query, err := gojq.Parse(jqQuery)
	if err != nil {
		return nil, fmt.Errorf("failed to parse jq query '%s': %w", jqQuery, err)
	}

	iter := query.Run(data)
	v, ok := iter.Next()
	if !ok {
		return nil, fmt.Errorf("no results from jq query '%s'", jqQuery)
	}
	if err, isErr := v.(error); isErr {
		return nil, fmt.Errorf("jq query error: %w", err)
	}

	if resultMap, ok := v.(map[string]interface{}); ok {
		return resultMap, nil
	}

	return nil, fmt.Errorf("unexpected result type from jq query: %T", v)
}

// joinErrors joins multiple errors into one. Uses errors.Join if available, otherwise concatenates messages.
func joinErrors(errs []error) error {
	if len(errs) == 0 {
		return nil
	}
	// Use errors.Join (Go 1.20+)
	return errors.Join(errs...)
}

// applyPathValueToEntriesWithGojq applies a value to entries using jq DSL
func (r *Resolver) applyPathValueToEntriesWithGojq(jqQuery string, value string, entries []registry.Entry) {
	err := applyPathValueToEntriesWithGojq(jqQuery, value, entries)
	if err != nil {
		r.logger.Warn("failed to apply path value to entries with gojq",
			zap.String("jqQuery", jqQuery),
			zap.Error(err))
	}
}

func findDefinitionDependency(nsDefinition registry.Entry, nsDependencies map[string]registry.Entry) (registry.Entry, string, error) {
	reqData := nsDefinition.Data.Data()
	reqMap, ok := reqData.(map[string]interface{})
	if !ok {
		return registry.Entry{}, "", fmt.Errorf("invalid definition data in requirement %s", nsDefinition.ID.Name)
	}

	targetsRaw, ok := reqMap["targets"].([]interface{})
	if !ok {
		return registry.Entry{}, "", fmt.Errorf("invalid definition data in requirement %s", nsDefinition.ID.Name)
	}

	// Iterate through all targets to find one that matches a dependency
	for _, targetRaw := range targetsRaw {
		if targetMap, ok := targetRaw.(map[string]interface{}); ok {
			// Check if the target entry matches any dependency name
			if entryName, ok := targetMap["entry"].(string); ok {
				for _, nsDependency := range nsDependencies {
					if entryName == nsDependency.ID.Name &&
						nsDefinition.ID.NS == nsDependency.ID.NS {
						if path, ok := targetMap["path"].(string); ok {
							return nsDependency, path, nil
						}
					}
				}
			}
		}
	}

	return registry.Entry{}, "", fmt.Errorf("dependency for definition %s not found", nsDefinition.ID.Name)
}

func getValueFromEntry(entry registry.Entry, path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("path cannot be empty")
	}

	// Get the data from the entry
	data := entry.Data.Data()
	if data == nil {
		return "", fmt.Errorf("entry data is nil")
	}

	// Use gojq to extract the value
	value, err := getValueFromEntryWithGojq(data, path)
	if err != nil {
		return "", fmt.Errorf("failed to extract value with gojq from path '%s': %w", path, err)
	}

	return value, nil
}

// getValueFromEntryWithGojq uses gojq to extract values from data using jq-style queries
func getValueFromEntryWithGojq(data interface{}, path string) (string, error) {
	// Parse the jq query
	query, err := gojq.Parse(path)
	if err != nil {
		return "", fmt.Errorf("failed to parse jq query '%s': %w", path, err)
	}

	// Run the query
	iter := query.Run(data)

	// Get the first result
	v, ok := iter.Next()
	if !ok || v == nil {
		return "", fmt.Errorf("no results found for query '%s'", path)
	}

	// Check for errors
	if err, ok := v.(error); ok {
		return "", fmt.Errorf("jq query error: %w", err)
	}

	// Convert the result to string
	switch val := v.(type) {
	case string:
		return val, nil
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return fmt.Sprintf("%d", val), nil
	case float32, float64:
		return fmt.Sprintf("%g", val), nil
	case bool:
		return fmt.Sprintf("%t", val), nil
	default:
		return fmt.Sprintf("%v", val), nil
	}
}

// DefinitionTarget represents a target in the new format
type DefinitionTarget struct {
	Entry string `json:"entry" yaml:"entry"`
	Path  string `json:"path" yaml:"path"`
}

func findRequirementTargetEntries(requirementTarget DefinitionTarget, ns string, entries []registry.Entry) []registry.Entry {
	results := make([]registry.Entry, 0)

	for _, entry := range entries {
		// Check if the entry ID matches the requirement target entry
		if entry.ID.NS == ns {
			if requirementTarget.Entry == "" {
				// When Entry is empty, match by Path field which contains path like "meta.depends_on[]"
				if requirementTarget.Path != "" {
					// For now, just add all entries in the namespace when Path is specified
					results = append(results, entry)
				}
				continue
			}

			// When Entry is specified, match by exact name
			if entry.ID.Name == requirementTarget.Entry {
				results = append(results, entry)
			}
		}
	}

	return results
}

func getRequirementTargets(requirement registry.Entry) ([]DefinitionTarget, error) {
	data := requirement.Data.Data()

	// Try to parse as raw map data (new format)
	if reqMap, ok := data.(map[string]interface{}); ok {
		// Extract targets from the raw map format
		if targetsRaw, ok := reqMap["targets"].([]interface{}); ok {
			targets := make([]DefinitionTarget, 0, len(targetsRaw))
			for _, targetRaw := range targetsRaw {
				if targetMap, ok := targetRaw.(map[string]interface{}); ok {
					target := DefinitionTarget{}
					if entry, ok := targetMap["entry"].(string); ok {
						target.Entry = entry
					}
					if path, ok := targetMap["path"].(string); ok {
						target.Path = path
					}
					targets = append(targets, target)
				}
			}
			return targets, nil
		}
	}

	return nil, fmt.Errorf("invalid Definition data in Requirement %s", requirement.ID.Name)
}

func findDefinitionRequirement(definition registry.Entry, nsRequirements map[string]registry.Entry) (registry.Entry, error) {
	requirement, ok := nsRequirements[definition.ID.Name]
	if !ok {
		return registry.Entry{}, fmt.Errorf("requirement for Definition %s not found", definition.ID.Name)
	}

	return requirement, nil
}
