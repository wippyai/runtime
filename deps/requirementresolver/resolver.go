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

// ResolveModuleDefinitions resolves module requirements and injects parameters with direct name matching
func (r *Resolver) ResolveModuleDefinitions(entries []registry.Entry) ([]registry.Entry, error) {
	nsRequirements := make(map[string]registry.Entry)
	nsDependencies := make(map[string]registry.Entry)

	// Collect all entries by kind
	for _, entry := range entries {
		switch entry.Kind {
		case registry.KindNamespaceDependency:
			nsDependencies[entry.ID.String()] = entry
		case registry.KindNamespaceRequirement:
			nsRequirements[entry.ID.String()] = entry
		}
	}

	// Process each requirement and find matching dependency parameters
	for _, nsRequirement := range nsRequirements {
		r.logger.Debug("processing requirement",
			zap.String("requirement_name", nsRequirement.ID.Name),
			zap.String("requirement_namespace", nsRequirement.ID.NS))

		// Resolve parameter value (from dependency or default)
		paramValue, err := r.resolveParameterValue(nsRequirement, nsDependencies)
		if err != nil {
			r.logger.Warn("failed to resolve parameter value for requirement",
				zap.String("requirement", nsRequirement.ID.Name),
				zap.Error(err))
			continue
		}

		// Get requirement targets for value injection
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

		// Apply the parameter value to target entries
		for _, requirementTarget := range requirementTargets {
			targetEntries := findRequirementTargetEntries(requirementTarget, nsRequirement.ID.NS, entries)

			r.logger.Debug("found target entries for requirement",
				zap.String("requirement", nsRequirement.ID.Name),
				zap.String("target_entry", requirementTarget.Entry),
				zap.String("target_path", requirementTarget.Path),
				zap.Int("target_count", len(targetEntries)),
				zap.Any("target_entry_ids", getEntryIDs(targetEntries)))

			// Apply the value to target entries using JSONPath (keeping this part for backward compatibility)
			err = ApplyPathValueToEntriesWithGojq(requirementTarget.Path, paramValue, targetEntries)
			if err != nil {
				r.logger.Debug("failed to apply value to target entries",
					zap.String("requirement", nsRequirement.ID.Name),
					zap.String("path", requirementTarget.Path),
					zap.String("value", paramValue),
					zap.Error(err))
				continue
			}

			// Update the original entries slice with the modified target entries
			updateEntriesWithTargetEntries(entries, targetEntries)

			// Verify that the value was actually injected
			r.verifyInjection(targetEntries, requirementTarget.Path, paramValue)
		}
	}

	// Validate that all dependency parameters have corresponding requirements
	if err := r.validateParameterMatching(nsDependencies, nsRequirements); err != nil {
		r.logger.Warn("parameter validation failed", zap.Error(err))
	}

	return entries, nil
}

// resolveParameterValue resolves a parameter value for a requirement from either dependency or default
func (r *Resolver) resolveParameterValue(requirement registry.Entry, dependencies map[string]registry.Entry) (string, error) {
	// Try to find parameter in dependencies first
	_, paramValue, err := findDependencyByParameterName(requirement, dependencies)
	if err == nil {
		r.logger.Debug("found dependency parameter for requirement",
			zap.String("requirement", requirement.ID.Name),
			zap.String("parameter_value", paramValue))
		return paramValue, nil
	}

	// Fall back to default value if available
	defaultValue, hasDefault := getRequirementDefaultValue(requirement)
	if hasDefault {
		r.logger.Debug("using default value for requirement",
			zap.String("requirement", requirement.ID.Name),
			zap.String("default_value", defaultValue))
		return defaultValue, nil
	}

	// No parameter found and no default available
	return "", fmt.Errorf("no parameter found and no default value available for requirement %s", requirement.ID.Name)
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

		// Determine update strategy based on query path
		trimmedQuery := strings.TrimSpace(jqQuery)
		if trimmedQuery == ".kind" {
			// Update entire entry for kind field
			err := updateEntireEntryWithGojq(entry, jqQuery, value)
			if err != nil {
				errs = append(errs, fmt.Errorf("failed to update entire entry with jq query '%s' for entry %s: %w", jqQuery, entry.ID.Name, err))
			}
		} else {
			// Try Data first, fallback to entire entry for meta fields
			err := updateEntryDataWithGojq(entry, jqQuery, value)
			if err != nil && strings.HasPrefix(trimmedQuery, ".meta") {
				// If Data update failed and it's a meta query, try entire entry
				err2 := updateEntireEntryWithGojq(entry, jqQuery, value)
				if err2 != nil {
					errs = append(errs, fmt.Errorf("failed to update entry with jq query '%s' for entry %s (data error: %w, entire entry error: %w)", jqQuery, entry.ID.Name, err, err2))
				}
			} else if err != nil {
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

// setValueWithGojqReturnMap with deduplication for depends_on arrays
func setValueWithGojqReturnMap(data interface{}, path string, value string) (map[string]interface{}, error) {
	trimmedPath := strings.TrimSpace(path)

	var jqQuery string
	if strings.Contains(trimmedPath, "+=") {
		// Special handling for depends_on to avoid duplicates
		if strings.Contains(trimmedPath, "depends_on") {
			// First check if value already exists
			checkQuery := strings.ReplaceAll(trimmedPath, "+=", "")
			checkQuery = strings.TrimSpace(checkQuery)

			// Try to get existing array
			query, err := gojq.Parse(checkQuery)
			if err == nil {
				iter := query.Run(data)
				if v, ok := iter.Next(); ok {
					if existingArray, isArray := v.([]interface{}); isArray {
						// Check if value already exists
						for _, existing := range existingArray {
							if existing == value {
								// Value already exists, return data unchanged
								if resultMap, ok := data.(map[string]interface{}); ok {
									return resultMap, nil
								}
								return nil, fmt.Errorf("data is not a map")
							}
						}
					}
				}
			}
		}

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

// findDependencyByParameterName finds a dependency with a parameter matching the given requirement name
// Uses meta.parent to find the specific parent dependency
func findDependencyByParameterName(requirement registry.Entry, nsDependencies map[string]registry.Entry) (registry.Entry, string, error) {
	requirementName := requirement.ID.Name

	// Check if meta.parent is not set
	if requirement.Meta == nil {
		return registry.Entry{}, "", fmt.Errorf("no meta.parent set for requirement %s", requirementName)
	}

	// Check if parent key exists
	parentID, exists := requirement.Meta["parent"]
	if !exists {
		return registry.Entry{}, "", fmt.Errorf("no meta.parent set for requirement %s", requirementName)
	}

	// Check if parent is a string
	parentIDStr, ok := parentID.(string)
	if !ok {
		return registry.Entry{}, "", fmt.Errorf("invalid meta.parent type for requirement %s", requirementName)
	}

	// Check if parent is not empty
	if parentIDStr == "" {
		return registry.Entry{}, "", fmt.Errorf("empty meta.parent for requirement %s", requirementName)
	}

	// Find the specific parent dependency by ID
	for _, nsDependency := range nsDependencies {
		if nsDependency.ID.String() == parentIDStr {
			paramValue, err := findParameterInDependency(requirementName, nsDependency)
			if err == nil {
				return nsDependency, paramValue, nil
			}
			return registry.Entry{}, "", fmt.Errorf("parameter %s not found in parent dependency %s", requirementName, parentIDStr)
		}
	}

	return registry.Entry{}, "", fmt.Errorf("parent dependency %s not found for requirement %s", parentIDStr, requirementName)
}

// findParameterInDependency searches for a parameter within a specific dependency
func findParameterInDependency(requirementName string, nsDependency registry.Entry) (string, error) {
	data := nsDependency.Data.Data()
	depMap, ok := data.(map[string]interface{})
	if !ok {
		return "", fmt.Errorf("invalid dependency data structure")
	}

	paramsRaw, exists := depMap["parameters"]
	if !exists || paramsRaw == nil {
		return "", fmt.Errorf("no parameters found in dependency")
	}

	paramsArray, ok := paramsRaw.([]interface{})
	if !ok {
		return "", fmt.Errorf("invalid parameters structure")
	}

	for _, paramRaw := range paramsArray {
		paramMap, ok := paramRaw.(map[string]interface{})
		if !ok {
			continue
		}

		paramName, ok := paramMap["name"].(string)
		if !ok || paramName != requirementName {
			continue
		}

		paramValue, ok := paramMap["value"].(string)
		if ok {
			return paramValue, nil
		}
	}

	return "", fmt.Errorf("parameter %s not found in dependency", requirementName)
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
		if requirementTarget.Entry == "" {
			// Add all entries in the namespace when Path is specified
			if requirementTarget.Path != "" {
				if entry.ID.NS == ns {
					results = append(results, entry)
				}
			}
			continue
		}

		// Check if the entry reference contains a namespace prefix
		if strings.Contains(requirementTarget.Entry, ":") {
			// Cross-namespace entry reference (namespace:entry)
			// Parse the namespace:entry format
			parts := strings.SplitN(requirementTarget.Entry, ":", 2)
			if len(parts) == 2 {
				targetNS := parts[0]
				targetName := parts[1]
				// Add entry from target namespace
				if entry.ID.NS == targetNS && entry.ID.Name == targetName {
					results = append(results, entry)
				}
			}
		} else if entry.ID.NS == ns && entry.ID.Name == requirementTarget.Entry {
			// Local namespace entry reference
			// Add entry when entry.ID.Name == requirementTarget.Entry
			results = append(results, entry)
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

// getRequirementDefaultValue extracts the default value from a requirement entry
func getRequirementDefaultValue(requirement registry.Entry) (string, bool) {
	data := requirement.Data.Data()
	if reqMap, ok := data.(map[string]interface{}); ok {
		if defaultValue, ok := reqMap["default"].(string); ok {
			return defaultValue, true
		}
	}
	return "", false
}

// validateParameterMatching validates that dependency parameters have corresponding requirements
func (r *Resolver) validateParameterMatching(nsDependencies map[string]registry.Entry, nsRequirements map[string]registry.Entry) error {
	// Collect all parameter names from dependencies
	allParamNames := make(map[string]bool)
	for _, nsDependency := range nsDependencies {
		data := nsDependency.Data.Data()
		if depMap, ok := data.(map[string]interface{}); ok {
			if paramsRaw, ok := depMap["parameters"].([]interface{}); ok {
				for _, paramRaw := range paramsRaw {
					if paramMap, ok := paramRaw.(map[string]interface{}); ok {
						if paramName, ok := paramMap["name"].(string); ok {
							allParamNames[paramName] = true
						}
					}
				}
			}
		}
	}

	// Check if each parameter has a corresponding requirement
	var missingRequirements []string
	for paramName := range allParamNames {
		found := false
		for _, nsRequirement := range nsRequirements {
			if nsRequirement.ID.Name == paramName {
				found = true
				break
			}
		}
		if !found {
			missingRequirements = append(missingRequirements, paramName)
		}
	}

	if len(missingRequirements) > 0 {
		return fmt.Errorf("dependency parameters have no corresponding requirements: %v", missingRequirements)
	}

	return nil
}
