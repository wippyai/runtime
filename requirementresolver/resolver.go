package requirementresolver

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/itchyny/gojq"
	"github.com/ponyruntime/pony/api/payload"
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
func (r *Resolver) ResolveModuleDefinitions(entries []registry.Entry) ([]registry.Entry, error) {
	nsRequirements := make(map[string]registry.Entry)
	nsDependencies := make(map[string]registry.Entry)
	nsDefinitions := make(map[string]registry.Entry)

	// Collect all entries by kind
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

	// Log available requirements and definitions for debugging
	r.logger.Debug("requirement resolver debug info",
		zap.Any("available_requirements", getEntryNames(nsRequirements)),
		zap.Any("available_definitions", getEntryNames(nsDefinitions)),
		zap.Any("available_dependencies", getEntryNames(nsDependencies)))

	for _, nsDefinition := range nsDefinitions {
		r.logger.Debug("processing definition",
			zap.String("definition_name", nsDefinition.ID.Name),
			zap.String("definition_namespace", nsDefinition.ID.NS))

		nsDependency, path, err := findDefinitionDependency(nsDefinition, nsDependencies)
		if err != nil {
			r.logger.Warn("failed to find definition dependency",
				zap.String("definition", nsDefinition.ID.Name),
				zap.Error(err))
			continue
		}

		r.logger.Debug("found dependency for definition",
			zap.String("definition", nsDefinition.ID.Name),
			zap.String("dependency", nsDependency.ID.Name),
			zap.String("path", path))

		value, err := getValueFromEntry(nsDependency, path)
		if err != nil {
			r.logger.Warn("failed to get value from entry",
				zap.String("dependency", nsDependency.ID.Name),
				zap.String("path", path),
				zap.Error(err))
			continue
		}

		r.logger.Debug("extracted value from dependency",
			zap.String("definition", nsDefinition.ID.Name),
			zap.String("value", value))

		nsRequirement, err := findDefinitionRequirement(nsDefinition, nsRequirements)
		if err != nil {
			r.logger.Warn("failed to find Definition Requirement",
				zap.String("Definition", nsDefinition.ID.Name),
				zap.String("available_requirements", fmt.Sprintf("%v", getEntryNames(nsRequirements))),
				zap.Error(err))
			continue
		}

		r.logger.Debug("found requirement for definition",
			zap.String("definition", nsDefinition.ID.Name),
			zap.String("requirement", nsRequirement.ID.Name),
			zap.String("requirement_namespace", nsRequirement.ID.NS))

		requirementTargets, err := getRequirementTargets(nsRequirement)
		if err != nil {
			r.logger.Warn("failed to get requirement targets",
				zap.String("requirement", nsRequirement.ID.Name),
				zap.Error(err))
			continue
		}

		r.logger.Debug("found requirement targets",
			zap.String("requirement", nsRequirement.ID.Name),
			zap.Any("targets", requirementTargets))

		for _, requirementTarget := range requirementTargets {
			targetEntries := findRequirementTargetEntries(requirementTarget, nsRequirement.ID.NS, entries)

			r.logger.Debug("found target entries for requirement",
				zap.String("requirement", nsRequirement.ID.Name),
				zap.String("target_entry", requirementTarget.Entry),
				zap.String("target_path", requirementTarget.Path),
				zap.Int("target_count", len(targetEntries)),
				zap.Any("target_entry_ids", getEntryIDs(targetEntries)))

			// Apply the value to target entries
			err = ApplyPathValueToEntriesWithGojq(requirementTarget.Path, value, targetEntries)
			if err != nil {
				r.logger.Debug("failed to apply value to target entries",
					zap.String("requirement", nsRequirement.ID.Name),
					zap.String("path", requirementTarget.Path),
					zap.String("value", value),
					zap.Error(err))
				continue
			}

			// Update the original entries slice with the modified target entries
			updateEntriesWithTargetEntries(entries, targetEntries)

			// Verify that the value was actually injected
			r.verifyInjection(targetEntries, requirementTarget.Path, value)
		}
	}

	return entries, nil
}

// ApplyPathValueToEntriesWithGojq applies a value to entries using jq DSL queries
// Now works with raw JSON for the entire entry
func ApplyPathValueToEntriesWithGojq(jqQuery string, value string, entries []registry.Entry) error {
	errs := make([]error, 0)
	for i := range entries {
		entry := &entries[i]

		// Handle special cases for backward compatibility
		if jqQuery == "kind" {
			entry.Kind = value
			continue
		}

		// Check if the jqQuery targets kind or meta fields to determine update strategy
		trimmedQuery := strings.TrimSpace(jqQuery)
		if trimmedQuery == ".kind" || strings.HasPrefix(trimmedQuery, ".meta") {
			// Update entire object (including kind and meta fields)
			err := updateEntireEntryWithGojq(entry, jqQuery, value)
			if err != nil {
				errs = append(errs, fmt.Errorf("failed to update entire entry with jq query '%s' for entry %s: %w", jqQuery, entry.ID.Name, err))
			}
		} else {
			// Update only the Data field (including .data queries)
			err := updateEntryDataWithGojq(entry, jqQuery, value)
			if err != nil {
				errs = append(errs, fmt.Errorf("failed to update entry data with jq query '%s' for entry %s: %w", jqQuery, entry.ID.Name, err))
			}
		}
	}
	if len(errs) > 0 {
		return joinErrors(errs)
	}
	return nil
}

// updateEntireEntryWithGojq updates the entire entry object using jq queries
func updateEntireEntryWithGojq(entry *registry.Entry, jqQuery string, value string) error {
	// Convert entry to raw JSON map (excluding Data field to avoid interface serialization issues)
	var data interface{}
	if entry.Data != nil {
		data = entry.Data.Data()
	}
	entry.Data = nil

	entryMap, err := entryToRawJSONMap(entry)
	if err != nil {
		return fmt.Errorf("failed to convert entry to JSON map: %w", err)
	}

	// Apply jq query to the raw JSON map
	updatedMap, err := setValueWithGojqReturnMap(entryMap, jqQuery, value)
	if err != nil {
		return fmt.Errorf("failed to set value with jq query: %w", err)
	}

	// Convert the modified map back to registry.Entry
	err = updateEntryFromRawJSONMap(entry, updatedMap)
	if err != nil {
		return fmt.Errorf("failed to update entry from JSON map: %w", err)
	}

	// Only restore Data if it was originally not nil
	if data != nil {
		entry.Data = payload.New(data)
	}

	return nil
}

// updateEntryDataWithGojq updates only the Data field of an entry using jq queries
func updateEntryDataWithGojq(entry *registry.Entry, jqQuery string, value string) error {
	// Get the current data from the entry
	currentData := entry.Data.Data()
	if currentData == nil {
		// If no data exists, create an empty map
		currentData = make(map[string]interface{})
	}

	// Handle .data queries by stripping the .data prefix
	trimmedQuery := strings.TrimSpace(jqQuery)
	if strings.HasPrefix(trimmedQuery, ".data") {
		// Remove the .data prefix and apply the query to the data
		dataQuery := strings.TrimPrefix(trimmedQuery, ".data")
		if dataQuery == "" {
			// If query is just ".data", set the entire data field
			entry.Data = payload.New(value)
			return nil
		}
		// Apply the query to the data field
		updatedData, err := setValueWithGojqReturnMap(currentData, dataQuery, value)
		if err != nil {
			return fmt.Errorf("failed to set value with jq query: %w", err)
		}
		entry.Data = payload.New(updatedData)
		return nil
	}

	// Apply jq query to the data
	updatedData, err := setValueWithGojqReturnMap(currentData, jqQuery, value)
	if err != nil {
		return fmt.Errorf("failed to set value with jq query: %w", err)
	}

	// Create a new payload with the updated data
	entry.Data = payload.New(updatedData)

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

// updateEntriesWithTargetEntries updates the original entries slice with modified target entries
func updateEntriesWithTargetEntries(entries []registry.Entry, targetEntries []registry.Entry) {
	// Create a map of target entries by ID for efficient lookup
	targetMap := make(map[string]registry.Entry)
	for _, targetEntry := range targetEntries {
		targetMap[targetEntry.ID.String()] = targetEntry
	}

	// Update the original entries slice with the modified target entries
	for i := range entries {
		if updatedEntry, exists := targetMap[entries[i].ID.String()]; exists {
			entries[i] = updatedEntry
		}
	}
}

// joinErrors joins multiple errors into one. Uses errors.Join if available, otherwise concatenates messages.
func joinErrors(errs []error) error {
	if len(errs) == 0 {
		return nil
	}
	// Use errors.Join (Go 1.20+)
	return errors.Join(errs...)
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

// getEntryNames returns a slice of entry names for debugging
func getEntryNames(entries map[string]registry.Entry) []string {
	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	return names
}

// getEntryIDs returns a slice of entry IDs for debugging
func getEntryIDs(entries []registry.Entry) []string {
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		ids = append(ids, entry.ID.String())
	}
	return ids
}

// verifyInjection verifies that a value was actually injected into target entries
func (r *Resolver) verifyInjection(entries []registry.Entry, path string, expectedValue string) {
	path = strings.ReplaceAll(path, "+=", "")
	path = strings.TrimSpace(path)

	for _, entry := range entries {
		var actualValue string
		var err error

		// Check if the path starts with ".kind" or ".meta" to determine verification strategy
		if path == ".kind" || strings.HasPrefix(path, ".meta") {
			// For kind and meta paths, verify against the entire entry object
			entryMap, err := entryToRawJSONMap(&entry)
			if err != nil {
				r.logger.Debug("failed to convert entry for verification",
					zap.String("entry_id", entry.ID.String()),
					zap.Error(err))
				continue
			}

			// Extract the actual value from the path
			actualValue, _ = getValueFromEntryWithGojq(entryMap, path)
		} else {
			// For data paths, verify against the Data field only
			data := entry.Data.Data()
			if data == nil {
				r.logger.Debug("entry data is nil for verification",
					zap.String("entry_id", entry.ID.String()),
					zap.String("path", path))
				continue
			}

			r.logger.Debug("modified entry", zap.Any("data", data))

			// Extract the actual value from the data
			actualValue, err = getValueFromEntryWithGojq(data, path)
		}

		if err != nil {
			r.logger.Debug("failed to extract value for verification",
				zap.String("entry_id", entry.ID.String()),
				zap.String("path", path),
				zap.Error(err))
			continue
		}

		if !strings.Contains(actualValue, expectedValue) {
			r.logger.Debug("injection verification failed",
				zap.String("entry_id", entry.ID.String()),
				zap.String("path", path),
				zap.String("expected_value", expectedValue),
				zap.String("actual_value", actualValue))
		} else {
			r.logger.Debug("injection verification successful",
				zap.String("entry_id", entry.ID.String()),
				zap.String("path", path),
				zap.String("value", actualValue))
		}
	}
}
