package requirementresolver

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/ponyruntime/pony/api/registry"
	"go.uber.org/zap"
)

// Resolver handles module requirement resolution
type Resolver struct {
	logger *zap.Logger
}

// NewResolver creates a new Resolver instance with the given logger
func NewResolver(logger *zap.Logger) *Resolver {
	return &Resolver{
		logger: logger,
	}
}

// ResolveModuleRequirements resolves module requirements and injects parameters
func (r *Resolver) ResolveModuleRequirements(entries []registry.Entry) error {
	nsDefinitions := make(map[string]registry.Entry)
	nsDependencies := make(map[string]registry.Entry)
	nsRequirements := make(map[string]registry.Entry)

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

	for _, nsRequirement := range nsRequirements {
		nsDependency, path, err := findRequirementDependency(nsRequirement, nsDependencies)
		if err != nil {
			r.logger.Warn("failed to find requirement dependency",
				zap.String("requirement", nsRequirement.ID.Name),
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

		nsDefinition, err := findRequirementDefinition(nsRequirement, nsDefinitions)
		if err != nil {
			r.logger.Warn("failed to find requirement definition",
				zap.String("requirement", nsRequirement.ID.Name),
				zap.Error(err))
			continue
		}

		definitionTargets, err := getDefinitionTargets(nsDefinition)
		if err != nil {
			r.logger.Warn("failed to get definition targets",
				zap.String("definition", nsDefinition.ID.Name),
				zap.Error(err))
			continue
		}

		for _, definitionTarget := range definitionTargets {
			targetEntries := findDefinitionTargetEntries(definitionTarget, nsDefinition.ID.NS, entries)
			r.applyPathValueToEntries(definitionTarget.Value, value, targetEntries)
		}
	}

	return nil
}

// applyPathValueToEntries applies a value to entries at the specified path
func applyPathValueToEntries(targetPath string, value string, entries []registry.Entry) error {
	errs := make([]error, 0)
	for i := range entries {
		entry := &entries[i]

		// Dispatch based on path prefix
		if strings.HasPrefix(targetPath, "meta.") {
			if entry.Meta == nil {
				entry.Meta = make(map[string]interface{})
			}
			path := strings.TrimPrefix(targetPath, "meta.")
			err := setValueByPath(entry.Meta, path, value, strings.HasSuffix(targetPath, "[]"))
			if err != nil {
				// Log and collect error
				// (logging will be handled by the caller if needed)
				errs = append(errs, fmt.Errorf("failed to set value at path %s for entry %s: %w", targetPath, entry.ID.Name, err))
			}
			continue
		}
		if strings.HasPrefix(targetPath, "data.") {
			if entry.Data == nil {
				// Not handling Data field creation here
				continue
			}
			// Not implemented: Data field path support
			continue
		}
		if targetPath == "kind" {
			entry.Kind = value
			continue
		}
		// Fallback: try to set on Meta
		if entry.Meta == nil {
			entry.Meta = make(map[string]interface{})
		}
		err := setValueByPath(entry.Meta, targetPath, value, strings.HasSuffix(targetPath, "[]"))
		if err != nil {
			errs = append(errs, fmt.Errorf("failed to set value at path %s for entry %s: %w", targetPath, entry.ID.Name, err))
		}
	}
	if len(errs) > 0 {
		// Go 1.20+ errors.Join, otherwise join manually
		return joinErrors(errs)
	}
	return nil
}

// joinErrors joins multiple errors into one. Uses errors.Join if available, otherwise concatenates messages.
func joinErrors(errs []error) error {
	if len(errs) == 0 {
		return nil
	}
	// Use errors.Join (Go 1.20+)
	return errors.Join(errs...)
}

func (r *Resolver) applyPathValueToEntries(targetPath string, value string, entries []registry.Entry) {
	err := applyPathValueToEntries(targetPath, value, entries)
	if err != nil {
		r.logger.Warn("failed to apply path value to entries",
			zap.String("path", targetPath),
			zap.Error(err))
	}
}

// setValueByPath sets or appends a value at the given dynamic path in the entry, supporting nested maps and array filters
func setValueByPath(target interface{}, path string, value string, isAppend bool) error {
	parts := parsePath(path)
	if len(parts) == 0 {
		return fmt.Errorf("invalid path syntax")
	}
	return setNestedValueByPath(target, parts, value, isAppend)
}

// setNestedValueByPath traverses and sets a value in nested maps/slices, supporting array filters
func setNestedValueByPath(target interface{}, parts []interface{}, value string, isAppend bool) error {
	if len(parts) == 0 {
		return nil
	}

	// If target is registry.Metadata, treat as map[string]interface{}
	switch t := target.(type) {
	case map[string]interface{}:
		return setNestedValueByPathMap(t, parts, value, isAppend)
	case registry.Metadata:
		return setNestedValueByPathMap(map[string]interface{}(t), parts, value, isAppend)
	case *registry.Metadata:
		return setNestedValueByPathMap(map[string]interface{}(*t), parts, value, isAppend)
	default:
		return setNestedValueByPathMap(target, parts, value, isAppend)
	}
}

// setNestedValueByPathMap is the actual implementation for map[string]interface{}
func setNestedValueByPathMap(target interface{}, parts []interface{}, value string, isAppend bool) error {
	var curr = target
	var parent interface{}
	var parentKey interface{}

	for i, part := range parts {
		isLast := i == len(parts)-1

		switch p := part.(type) {
		case string:
			if p == "__append__" {
				// Append to the current slice
				slice, ok := curr.([]interface{})
				if !ok {
					return fmt.Errorf("expected slice for append, got %T", curr)
				}
				slice = append(slice, value)
				curr = slice
				// Update parent
				if parent != nil {
					switch pk := parentKey.(type) {
					case string:
						parent.(map[string]interface{})[pk] = curr
					case int:
						parent.([]interface{})[pk] = curr
					}
				}
				return nil
			}
			m, ok := curr.(map[string]interface{})
			if !ok {
				return fmt.Errorf("expected map at %v, got %T", p, curr)
			}
			if isLast {
				if isAppend {
					arr, _ := m[p].([]interface{})
					m[p] = append(arr, value)
				} else {
					m[p] = value
				}
				return nil
			}
			if _, exists := m[p]; !exists {
				if i+1 < len(parts) {
					switch parts[i+1].(type) {
					case *arrayFilter:
						m[p] = []interface{}{}
					case string:
						if parts[i+1] == "__append__" {
							m[p] = []interface{}{}
						} else {
							m[p] = map[string]interface{}{}
						}
					default:
						m[p] = map[string]interface{}{}
					}
				}
			}
			parent = curr
			parentKey = p
			curr = m[p]
		case *arrayFilter:
			slice, ok := curr.([]interface{})
			if !ok {
				return fmt.Errorf("expected slice for array filter, got %T", curr)
			}
			found := false
			for _, item := range slice {
				itemMap, ok := item.(map[string]interface{})
				if !ok {
					continue
				}
				if matchesFilter(itemMap, p) {
					if isLast {
						if isAppend {
							return fmt.Errorf("cannot append to array filter result directly")
						}
						return setNestedValueByPathMap(itemMap, parts[i+1:], value, isAppend)
					}
					curr = itemMap
					found = true
					break
				}
			}
			if !found {
				return fmt.Errorf("no items match filter '%s'", p.String())
			}
		case int:
			return fmt.Errorf("integer index not supported in path")
		default:
			return fmt.Errorf("invalid path part type: %T", part)
		}
	}
	return nil
}

func findRequirementDependency(nsRequirement registry.Entry, nsDependencies map[string]registry.Entry) (registry.Entry, string, error) {
	reqData := nsRequirement.Data.Data()
	reqMap, ok := reqData.(map[string]interface{})
	if !ok {
		return registry.Entry{}, "", fmt.Errorf("invalid requirement data in definition %s", nsRequirement.ID.Name)
	}

	targetsRaw, ok := reqMap["targets"].([]interface{})
	if !ok {
		return registry.Entry{}, "", fmt.Errorf("invalid requirement data in definition %s", nsRequirement.ID.Name)
	}

	// Iterate through all targets to find one that matches a dependency
	for _, targetRaw := range targetsRaw {
		if targetMap, ok := targetRaw.(map[string]interface{}); ok {
			// Check if the target entry matches any dependency name
			if entryName, ok := targetMap["entry"].(string); ok {
				for _, nsDependency := range nsDependencies {
					if entryName == nsDependency.ID.Name &&
						nsRequirement.ID.NS == nsDependency.ID.NS {
						// The target map has "path" field, not "value"
						if path, ok := targetMap["path"].(string); ok {
							return nsDependency, path, nil
						}
					}
				}
			}
		}
	}

	return registry.Entry{}, "", fmt.Errorf("dependency for requirement %s not found", nsRequirement.ID.Name)
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

	// Parse the path and navigate to the target value
	value, err := navigatePath(data, path)
	if err != nil {
		return "", fmt.Errorf("failed to navigate path '%s': %w", path, err)
	}

	// Convert the value to string
	if value == nil {
		return "", nil
	}

	switch v := value.(type) {
	case string:
		return v, nil
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return fmt.Sprintf("%d", v), nil
	case float32, float64:
		return fmt.Sprintf("%g", v), nil
	case bool:
		return fmt.Sprintf("%t", v), nil
	default:
		return fmt.Sprintf("%v", v), nil
	}
}

// navigatePath navigates through the data structure using the jq-style path
func navigatePath(data interface{}, path string) (interface{}, error) {
	parts := parsePath(path)
	if parts == nil {
		return nil, fmt.Errorf("invalid path syntax")
	}
	current := data

	for i, part := range parts {
		isLast := i == len(parts)-1

		switch p := part.(type) {
		case string:
			// Simple field access
			if mapData, ok := current.(map[string]interface{}); ok {
				if value, exists := mapData[p]; exists {
					current = value
				} else {
					return nil, fmt.Errorf("field '%s' not found", p)
				}
			} else {
				return nil, fmt.Errorf("cannot access field '%s' on non-map type %T", p, current)
			}

		case *arrayFilter:
			// Array filtering with condition
			if sliceData, ok := current.([]interface{}); ok {
				filtered := make([]interface{}, 0)
				for _, item := range sliceData {
					if itemMap, ok := item.(map[string]interface{}); ok {
						if matchesFilter(itemMap, p) {
							filtered = append(filtered, item)
						}
					}
				}

				if len(filtered) == 0 {
					return nil, fmt.Errorf("no items match filter '%s'", p.String())
				}

				if !isLast && len(filtered) > 1 {
					return nil, fmt.Errorf("multiple items match filter '%s', expected exactly one", p.String())
				}
				current = filtered[0]
			} else {
				return nil, fmt.Errorf("cannot apply filter '%s' on non-slice type %T", p.String(), current)
			}

		default:
			return nil, fmt.Errorf("invalid path part type: %T", part)
		}
	}

	return current, nil
}

// parsePath parses a jq-style path into parts
func parsePath(path string) []interface{} {
	var parts []interface{}
	var current strings.Builder
	var inBracket bool
	var bracketContent strings.Builder

	for i := 0; i < len(path); i++ {
		char := path[i]

		switch char {
		case '.':
			if !inBracket {
				if current.Len() > 0 {
					parts = append(parts, current.String())
					current.Reset()
				}
			} else {
				bracketContent.WriteByte(char)
			}

		case '[':
			if !inBracket {
				inBracket = true
				if current.Len() > 0 {
					parts = append(parts, current.String())
					current.Reset()
				}
			} else {
				bracketContent.WriteByte(char)
			}

		case ']':
			if inBracket {
				inBracket = false
				if bracketContent.Len() == 0 {
					// Special case: [] means append
					parts = append(parts, "__append__")
				} else {
					filter := parseArrayFilter(bracketContent.String())
					parts = append(parts, filter)
				}
				bracketContent.Reset()
			} else {
				return nil // Invalid path - unmatched closing bracket
			}

		default:
			if inBracket {
				bracketContent.WriteByte(char)
			} else {
				current.WriteByte(char)
			}
		}
	}

	// Add the last part if there's anything left
	if current.Len() > 0 {
		parts = append(parts, current.String())
	}

	// Check for unmatched opening bracket
	if inBracket {
		return nil // Invalid path - unmatched opening bracket
	}

	return parts
}

// arrayFilter represents a filter condition for array elements
type arrayFilter struct {
	field    string
	operator string
	value    string
}

func (af *arrayFilter) String() string {
	return fmt.Sprintf("[%s%s%s]", af.field, af.operator, af.value)
}

// parseArrayFilter parses array filter expressions like "name=text"
func parseArrayFilter(filter string) *arrayFilter {
	// Handle different operators: =, !=, >, <, >=, <=
	operators := []string{"!=", ">=", "<=", "=", ">", "<"}

	for _, op := range operators {
		if strings.Contains(filter, op) {
			parts := strings.SplitN(filter, op, 2)
			if len(parts) == 2 {
				return &arrayFilter{
					field:    strings.TrimSpace(parts[0]),
					operator: op,
					value:    strings.TrimSpace(parts[1]),
				}
			}
		}
	}

	// Default to equality if no operator found
	return &arrayFilter{
		field:    strings.TrimSpace(filter),
		operator: "=",
		value:    "",
	}
}

// matchesFilter checks if a map matches the given filter condition
func matchesFilter(item map[string]interface{}, filter *arrayFilter) bool {
	fieldValue, exists := item[filter.field]
	if !exists {
		return false
	}

	// Convert field value to string for comparison
	var fieldStr string
	switch v := fieldValue.(type) {
	case string:
		fieldStr = v
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		fieldStr = fmt.Sprintf("%d", v)
	case float32, float64:
		fieldStr = fmt.Sprintf("%g", v)
	case bool:
		fieldStr = fmt.Sprintf("%t", v)
	default:
		fieldStr = fmt.Sprintf("%v", v)
	}

	switch filter.operator {
	case "=":
		return fieldStr == filter.value
	case "!=":
		return fieldStr != filter.value
	case ">":
		return compareStrings(fieldStr, filter.value) > 0
	case "<":
		return compareStrings(fieldStr, filter.value) < 0
	case ">=":
		return compareStrings(fieldStr, filter.value) >= 0
	case "<=":
		return compareStrings(fieldStr, filter.value) <= 0
	default:
		return false
	}
}

// compareStrings compares two strings, trying to convert to numbers if possible
func compareStrings(a, b string) int {
	// Try to parse as numbers first
	if aNum, aErr := parseNumber(a); aErr == nil {
		if bNum, bErr := parseNumber(b); bErr == nil {
			if aNum < bNum {
				return -1
			} else if aNum > bNum {
				return 1
			}
			return 0
		}
	}

	// Fall back to string comparison
	return strings.Compare(a, b)
}

// parseNumber attempts to parse a string as a number
func parseNumber(s string) (float64, error) {
	return strconv.ParseFloat(strings.TrimSpace(s), 64)
}

// RequirementTarget represents a target in the new format
type RequirementTarget struct {
	Name  string `json:"name" yaml:"name"`
	Value string `json:"value" yaml:"value"`
}

func findDefinitionTargetEntries(definitionTarget RequirementTarget, ns string, entries []registry.Entry) []registry.Entry {
	results := make([]registry.Entry, 0)

	for _, entry := range entries {
		// Check if the entry ID matches the definition target name
		if entry.ID.NS == ns {
			if definitionTarget.Name == "" {
				// When Name is empty, match by Value field which contains path like "meta.depends_on[]"
				if definitionTarget.Value != "" {
					// For now, just add all entries in the namespace when Value is specified
					results = append(results, entry)
				}
				continue
			}

			// When Name is specified, match by exact name
			if entry.ID.Name == definitionTarget.Name {
				results = append(results, entry)
			}
		}
	}

	return results
}

func getDefinitionTargets(definition registry.Entry) ([]RequirementTarget, error) {
	data := definition.Data.Data()

	// Try to parse as raw map data (new format)
	if reqMap, ok := data.(map[string]interface{}); ok {
		// Extract targets from the raw map format
		if targetsRaw, ok := reqMap["targets"].([]interface{}); ok {
			targets := make([]RequirementTarget, 0, len(targetsRaw))
			for _, targetRaw := range targetsRaw {
				if targetMap, ok := targetRaw.(map[string]interface{}); ok {
					target := RequirementTarget{}
					if name, ok := targetMap["name"].(string); ok {
						target.Name = name
					}
					if value, ok := targetMap["value"].(string); ok {
						target.Value = value
					}
					targets = append(targets, target)
				}
			}
			return targets, nil
		}
	}

	return nil, fmt.Errorf("invalid requirement data in definition %s", definition.ID.Name)
}

func findRequirementDefinition(requirement registry.Entry, nsDefinitions map[string]registry.Entry) (registry.Entry, error) {
	definition, ok := nsDefinitions[requirement.ID.Name]
	if !ok {
		return registry.Entry{}, fmt.Errorf("definition for requirement %s not found", requirement.ID.Name)
	}

	return definition, nil
}

func findDependencyRequirements(nsDependency registry.Entry, nsRequirements map[string]registry.Entry) []registry.Entry {
	matchingRequirements := make([]registry.Entry, 0)

	for _, nsRequirement := range nsRequirements {
		reqData := nsRequirement.Data.Data()

		// Parse as raw map data (new format)
		if reqMap, ok := reqData.(map[string]interface{}); ok {
			// Extract targets from the raw map format
			if targetsRaw, ok := reqMap["targets"].([]interface{}); ok {
				for _, targetRaw := range targetsRaw {
					if targetMap, ok := targetRaw.(map[string]interface{}); ok {
						// Check if the target entry matches the dependency name
						if entryName, ok := targetMap["entry"].(string); ok {
							if entryName == nsDependency.ID.Name &&
								nsRequirement.ID.NS == nsDependency.ID.NS {
								matchingRequirements = append(matchingRequirements, nsRequirement)
								break
							}
						}
					}
				}
			}
		}
	}

	return matchingRequirements
}

type Result struct {
	ProviderEntry registry.Entry
	TargetEntry   registry.Entry
}
