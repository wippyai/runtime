package topology

import (
	"strings"

	"github.com/ponyruntime/pony/api/registry"
)

// PathConfig defines a dependency path configuration
type PathConfig struct {
	Path          string // Dot-notation path (e.g., "data.server", "meta.router")
	Description   string // Documentation about this dependency path
	AllowWildcard bool   // Whether wildcards (*) are permitted in this path
}

// DependencyPaths defines all possible paths where dependencies can be found
var DependencyPaths = []PathConfig{
	// Meta section dependencies
	{Path: "meta.server", Description: "Reference to HTTP server"},
	{Path: "meta.router", Description: "Reference to router component"},
	{Path: "meta.depends_on", Description: "Explicit dependencies in metadata"},

	// Data section direct references
	{Path: "data.server", Description: "Reference to HTTP server in data section"},
	{Path: "data.fs", Description: "Reference to filesystem"},
	{Path: "data.store", Description: "Reference to a store (e.g., 'session')"},
	{Path: "data.token_store", Description: "Reference to token storage"},
	{Path: "data.set", Description: "Reference to a template set"},
	{Path: "data.host", Description: "Reference to a host component"},
	{Path: "data.process", Description: "Reference to a process component"},
	{Path: "data.bucket", Description: "Reference to a storage bucket"},
	{Path: "data.config", Description: "Reference to a configuration entry"},

	// Array dependencies
	{Path: "data.middleware", Description: "List of middleware components", AllowWildcard: true},
	{Path: "data.post_middleware", Description: "List of post-middleware components", AllowWildcard: true},

	// Nested and complex dependencies
	{Path: "data.imports.*", Description: "Imported components (values only)", AllowWildcard: true},
	{Path: "data.*.depends_on", Description: "Explicit dependencies in nested structures", AllowWildcard: true},

	// Lifecycle dependencies
	{Path: "data.lifecycle.depends_on", Description: "Lifecycle dependencies"},

	// Security-related dependencies
	{Path: "data.lifecycle.security.policies", Description: "Security policies", AllowWildcard: true},
	{Path: "data.lifecycle.security.groups", Description: "Security groups", AllowWildcard: true},
	{Path: "data.security.policies", Description: "Direct security policies", AllowWildcard: true},
	{Path: "data.security.groups", Description: "Direct security groups", AllowWildcard: true},
	{Path: "data.security.token_store", Description: "Token store reference"},
}

// ExtractDependencies extracts all dependencies from a data structure
func ExtractDependencies(data any) []string {
	var deps []string

	// Process map-like structures
	if m, ok := data.(map[string]any); ok {
		for _, pathConfig := range DependencyPaths {
			pathDeps := extractFromPath(m, pathConfig.Path, pathConfig.AllowWildcard)
			deps = append(deps, pathDeps...)
		}
	}

	// Handle array of maps
	if arr, ok := data.([]any); ok {
		for _, item := range arr {
			if m, ok := item.(map[string]any); ok {
				itemDeps := ExtractDependencies(m)
				deps = append(deps, itemDeps...)
			}
		}
	}

	return removeDuplicates(deps)
}

// extractFromPath extracts dependencies by following a dot-notation path
func extractFromPath(data map[string]any, path string, allowWildcard bool) []string {
	segments := strings.Split(path, ".")
	if len(segments) == 0 {
		return nil
	}

	return navigatePath(data, segments, 0, allowWildcard)
}

// navigatePath recursively follows path segments to find dependencies
func navigatePath(data map[string]any, segments []string, index int, allowWildcard bool) []string {
	if index >= len(segments) {
		return nil
	}

	segment := segments[index]
	isLastSegment := index == len(segments)-1

	// Handle wildcard segment
	if segment == "*" && allowWildcard {
		var deps []string
		for _, value := range data {
			valueDeps := processSegmentValue(value, segments, index, isLastSegment, allowWildcard)
			deps = append(deps, valueDeps...)
		}
		return deps
	}

	// Handle regular segment
	value, exists := data[segment]
	if !exists {
		return nil
	}

	return processSegmentValue(value, segments, index, isLastSegment, allowWildcard)
}

// processSegmentValue handles values based on their type and position in the path
func processSegmentValue(value any, segments []string, index int, isLastSegment bool, allowWildcard bool) []string {
	var deps []string

	switch v := value.(type) {
	case string:
		// String at leaf node is a dependency
		if isLastSegment && v != "" {
			return []string{v}
		}

	case []any:
		if isLastSegment {
			// Extract all string values from the array
			return extractStringsFromArray(v)
		} else {
			// Continue path traversal for each map in the array
			for _, item := range v {
				if m, ok := item.(map[string]any); ok {
					nestedDeps := navigatePath(m, segments, index+1, allowWildcard)
					deps = append(deps, nestedDeps...)
				}
			}
		}

	case []string:
		// String array at leaf node
		if isLastSegment {
			for _, s := range v {
				if s != "" {
					deps = append(deps, s)
				}
			}
		}

	case map[string]any:
		if isLastSegment {
			// Extract all string values from the map
			for _, mapValue := range v {
				if strValue, ok := mapValue.(string); ok && strValue != "" {
					deps = append(deps, strValue)
				}
			}
		} else {
			// Continue path traversal
			nestedDeps := navigatePath(v, segments, index+1, allowWildcard)
			deps = append(deps, nestedDeps...)
		}
	}

	return deps
}

// extractStringsFromArray extracts string values from an array
func extractStringsFromArray(arr []any) []string {
	var result []string

	for _, item := range arr {
		switch v := item.(type) {
		case string:
			if v != "" {
				result = append(result, v)
			}
		case map[string]any:
			// Extract string values from map entries
			for _, mapValue := range v {
				if strValue, ok := mapValue.(string); ok && strValue != "" {
					result = append(result, strValue)
				}
			}
		}
	}

	return result
}

// removeDuplicates removes duplicate strings from a slice
func removeDuplicates(slice []string) []string {
	seen := make(map[string]bool)
	unique := make([]string, 0, len(slice))

	for _, entry := range slice {
		if !seen[entry] {
			seen[entry] = true
			unique = append(unique, entry)
		}
	}

	return unique
}

// fetchDependencies extracts dependencies from an entry's payload data
// This function is specifically used by sort.go to find dependencies beyond
// what's already in the Meta.TagValue(registry.TagDependsOn) field
func fetchDependencies(entry registry.Entry) []string {
	// Only extract dependencies from payload data
	// (Meta.TagValue(registry.TagDependsOn) is already handled in sort.go)
	if entry.Data == nil {
		return nil
	}

	return ExtractDependencies(entry.Data.Data())
}
