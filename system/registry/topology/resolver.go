package topology

import (
	"fmt"
	"strings"
	"sync"

	"github.com/ponyruntime/pony/api/registry"
)

// PathConfig defines a dependency extraction pattern.
type PathConfig struct {
	Path          string
	Description   string
	AllowWildcard bool
}

// Resolver manages dependency pattern extraction from registry entries.
// It maintains a thread-safe collection of patterns that can be registered
// by components during boot or configured by users.
type Resolver struct {
	patterns []PathConfig
	mu       sync.RWMutex
}

// NewResolver creates a new dependency resolver.
// Patterns must be registered by components during boot.
func NewResolver() *Resolver {
	return &Resolver{
		patterns: make([]PathConfig, 0),
	}
}

// RegisterPattern adds a new dependency extraction pattern.
// Returns error if pattern is invalid or already registered.
// Implements registry.DependencyResolver interface.
func (r *Resolver) RegisterPattern(pattern registry.DependencyPattern) error {
	if pattern.Path == "" {
		return fmt.Errorf("pattern path cannot be empty")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Check for duplicates
	for _, p := range r.patterns {
		if p.Path == pattern.Path {
			return fmt.Errorf("pattern %q already registered", pattern.Path)
		}
	}

	// Convert to internal PathConfig
	r.patterns = append(r.patterns, PathConfig{
		Path:          pattern.Path,
		Description:   pattern.Description,
		AllowWildcard: pattern.AllowWildcard,
	})
	return nil
}

// Patterns returns a copy of all registered patterns.
func (r *Resolver) Patterns() []PathConfig {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]PathConfig, len(r.patterns))
	copy(result, r.patterns)
	return result
}

// Extract returns all dependency IDs from an entry based on registered patterns.
// This is the main entry point for dependency extraction.
func (r *Resolver) Extract(entry registry.Entry) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.fetchDeps(entry)
}

// fetchDeps extracts dependencies using the registered patterns.
// This function combines meta and data, then processes all patterns.
func (r *Resolver) fetchDeps(entry registry.Entry) []string {
	combined := make(map[string]interface{})

	// Merge meta
	for k, v := range entry.Meta {
		combined["meta."+k] = v
	}

	// Merge data - get the underlying data from payload
	if entry.Data != nil {
		if dataMap, ok := entry.Data.Data().(map[string]interface{}); ok {
			for k, v := range dataMap {
				combined["data."+k] = v
			}
		}
	}

	// Collect all dependencies
	depSet := make(map[string]struct{})

	for _, pathCfg := range r.patterns {
		deps := resolverExtractFromPath(combined, pathCfg.Path, pathCfg.AllowWildcard)
		for _, dep := range deps {
			if dep != "" {
				depSet[dep] = struct{}{}
			}
		}
	}

	// Convert to slice
	result := make([]string, 0, len(depSet))
	for dep := range depSet {
		result = append(result, dep)
	}

	return result
}

// getKeys returns keys from a map for debugging
func getKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// resolverExtractFromPath extracts string values from a specific path in the data.
func resolverExtractFromPath(data map[string]interface{}, path string, allowWildcard bool) []string {
	// Handle wildcards
	if allowWildcard && strings.Contains(path, "*") {
		var deps []string
		for key, value := range data {
			if resolverMatchPattern(key, path) {
				deps = append(deps, resolverProcessLeafValue(value)...)
			}
		}
		return deps
	}

	// Direct lookup for non-wildcard paths since we have flattened keys like "meta.router"
	if value, exists := data[path]; exists {
		return resolverProcessLeafValue(value)
	}
	return nil
}

// resolverMatchPattern checks if a key matches a pattern with wildcards.
func resolverMatchPattern(key, pattern string) bool {
	if strings.Contains(pattern, "*") {
		if strings.HasPrefix(pattern, "*") {
			suffix := strings.TrimPrefix(pattern, "*")
			return strings.HasSuffix(key, suffix)
		}
		if strings.HasSuffix(pattern, "*") {
			prefix := strings.TrimSuffix(pattern, "*")
			return strings.HasPrefix(key, prefix)
		}
	}

	return key == pattern
}

// resolverNavigatePath recursively navigates through nested data structures.
func resolverNavigatePath(currentData any, segments []string, index int, allowWildcard bool) []string {
	if index >= len(segments) {
		return resolverProcessLeafValue(currentData)
	}

	segment := segments[index]

	if allowWildcard && (segment == "*" || strings.Contains(segment, "*")) {
		var deps []string
		if currentMap, ok := currentData.(map[string]any); ok {
			if index >= len(segments)-1 {
				for key, value := range currentMap {
					if segment == "*" || resolverMatchPattern(key, segment) {
						deps = append(deps, resolverProcessLeafValue(value)...)
					}
				}
			} else {
				for key, value := range currentMap {
					if segment == "*" || resolverMatchPattern(key, segment) {
						valueDeps := resolverNavigatePath(value, segments, index+1, allowWildcard)
						deps = append(deps, valueDeps...)
					}
				}
			}
		} else if currentArray, ok := currentData.([]any); ok {
			if index >= len(segments)-1 {
				for _, item := range currentArray {
					deps = append(deps, resolverProcessLeafValue(item)...)
				}
			} else {
				for _, item := range currentArray {
					itemDeps := resolverNavigatePath(item, segments, index+1, allowWildcard)
					deps = append(deps, itemDeps...)
				}
			}
		}
		return deps
	}

	currentMap, ok := currentData.(map[string]any)
	if !ok {
		return nil
	}

	value, exists := currentMap[segment]
	if !exists {
		return nil
	}

	return resolverNavigatePath(value, segments, index+1, allowWildcard)
}

// resolverProcessLeafValue extracts dependencies from leaf values.
func resolverProcessLeafValue(value any) []string {
	var deps []string
	switch v := value.(type) {
	case string:
		if v != "" {
			deps = append(deps, v)
		}
	case []any:
		for _, item := range v {
			if strVal, ok := item.(string); ok && strVal != "" {
				deps = append(deps, strVal)
			}
		}
	case []string:
		for _, s := range v {
			if s != "" {
				deps = append(deps, s)
			}
		}
	case map[string]any:
		for _, mapValue := range v {
			if strValue, ok := mapValue.(string); ok && strValue != "" {
				deps = append(deps, strValue)
			}
		}
	}
	return deps
}
