// SPDX-License-Identifier: MPL-2.0

package topology

import (
	"sort"
	"strings"
	"sync"

	"github.com/wippyai/runtime/api/registry"
)

// PathConfig defines a dependency extraction pattern.
type PathConfig struct {
	Path          string
	Description   string
	segments      []string
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
// Returns error if pattern is invalid.
// Allows duplicate patterns from different components.
// Implements registry.DependencyResolver interface.
func (r *Resolver) RegisterPattern(pattern registry.DependencyPattern) error {
	if pattern.Path == "" {
		return ErrEmptyPatternPath
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Convert to internal PathConfig with cached segments
	pathCfg := PathConfig{
		Path:          pattern.Path,
		Description:   pattern.Description,
		AllowWildcard: pattern.AllowWildcard,
		segments:      strings.Split(pattern.Path, "."),
	}
	r.patterns = append(r.patterns, pathCfg)
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
	combined := make(map[string]any)

	if len(entry.Meta) > 0 {
		combined["meta"] = map[string]any(entry.Meta)
	}

	if entry.Data != nil {
		payloadData := entry.Data.Data()
		if payloadData != nil {
			combined["data"] = payloadData
		}
	}

	if len(combined) == 0 {
		return nil
	}

	// Collect deduplicated dependencies in a slice so the output is independent
	// of Go map iteration order. Callers feed this slice into the dependency
	// graph; a randomized order is not a correctness issue for the graph
	// itself, but it leaks into any downstream consumer that uses the slice
	// directly (e.g. tie-breaking when adding nodes), so we sort before
	// returning to keep the contract stable across runs.
	seen := make(map[string]struct{}, len(r.patterns)*2)
	result := make([]string, 0, len(r.patterns)*2)
	for _, pathCfg := range r.patterns {
		deps := resolverExtractFromPath(combined, pathCfg)
		for _, dep := range deps {
			if dep == "" {
				continue
			}
			if _, ok := seen[dep]; ok {
				continue
			}
			seen[dep] = struct{}{}
			result = append(result, dep)
		}
	}
	sort.Strings(result)

	return result
}

// resolverExtractFromPath extracts string values from a specific path in the data using cached segments.
func resolverExtractFromPath(data map[string]any, pathCfg PathConfig) []string {
	if len(pathCfg.segments) == 0 {
		return nil
	}
	return resolverNavigatePath(data, pathCfg.segments, 0, pathCfg.AllowWildcard)
}

// resolverMatchPattern checks if a key matches a pattern with wildcards.
func resolverMatchPattern(key, pattern string) bool {
	if !strings.Contains(pattern, "*") {
		return key == pattern
	}

	// Handle *suffix pattern (e.g., *_env, *_id)
	if strings.HasPrefix(pattern, "*") && !strings.Contains(pattern[1:], "*") {
		suffix := pattern[1:]
		return strings.HasSuffix(key, suffix)
	}

	// Handle prefix* pattern
	if strings.HasSuffix(pattern, "*") && !strings.Contains(pattern[:len(pattern)-1], "*") {
		prefix := pattern[:len(pattern)-1]
		return strings.HasPrefix(key, prefix)
	}

	// Handle prefix*suffix pattern (e.g., app*_env)
	if strings.Contains(pattern, "*") {
		parts := strings.SplitN(pattern, "*", 2)
		if len(parts) == 2 {
			prefix, suffix := parts[0], parts[1]
			return strings.HasPrefix(key, prefix) && strings.HasSuffix(key, suffix) && len(key) >= len(prefix)+len(suffix)
		}
	}

	return false
}

// resolverNavigatePath recursively navigates through nested data structures.
func resolverNavigatePath(currentData any, segments []string, index int, allowWildcard bool) []string {
	if index >= len(segments) {
		return resolverProcessLeafValue(currentData)
	}

	segment := segments[index]

	if allowWildcard && (segment == "*" || strings.Contains(segment, "*")) {
		deps := make([]string, 0, 4)
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
			deps = make([]string, 0, len(currentArray))
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
