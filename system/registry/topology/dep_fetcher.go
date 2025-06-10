package topology

import (
	"reflect"
	"strings"

	"github.com/ponyruntime/pony/api/registry"
)

type PathConfig struct {
	Path          string
	Description   string
	AllowWildcard bool
}

var DependencyPaths = []PathConfig{
	{Path: "meta.server", Description: "Reference to HTTP server in metadata"},
	{Path: "meta.router", Description: "Reference to router component in metadata"},
	{Path: "meta.parent", Description: "Reference to parent component in metadata"},
	{Path: "meta.depends_on", Description: "Explicit dependencies in metadata", AllowWildcard: true},
	{Path: "meta.groups", Description: "Group membership list in metadata", AllowWildcard: true},
	{Path: "data.server", Description: "Reference to HTTP server in data section"},
	{Path: "data.fs", Description: "Reference to filesystem"},
	{Path: "data.store", Description: "Reference to a store (e.g., 'session')"},
	{Path: "data.token_store", Description: "Reference to token storage"},
	{Path: "data.set", Description: "Reference to a template set"},
	{Path: "data.host", Description: "Reference to a host component"},
	{Path: "data.process", Description: "Reference to a process component"},
	{Path: "data.bucket", Description: "Reference to a storage bucket"},
	{Path: "data.config", Description: "Reference to a configuration entry"},
	{Path: "data.func", Description: "Reference to handler function"},
	{Path: "data.middleware", Description: "List of middleware components", AllowWildcard: true},
	{Path: "data.post_middleware", Description: "List of post-middleware components", AllowWildcard: true},
	{Path: "data.groups", Description: "Group membership list in data", AllowWildcard: true},
	{Path: "data.imports.*", Description: "Imported components (values only)", AllowWildcard: true},
	{Path: "data.*.depends_on", Description: "Explicit dependencies in nested structures", AllowWildcard: true},
	{Path: "data.lifecycle.depends_on", Description: "Lifecycle dependencies", AllowWildcard: true},
	{Path: "data.lifecycle.security.policies", Description: "Security policies", AllowWildcard: true},
	{Path: "data.lifecycle.security.groups", Description: "Security groups", AllowWildcard: true},
	{Path: "data.security.policies", Description: "Direct security policies", AllowWildcard: true},
	{Path: "data.security.groups", Description: "Direct security groups", AllowWildcard: true},
	{Path: "data.security.token_store", Description: "Token store reference"},
	// Contract binding dependencies - automatically detect contract and method references
	{Path: "data.contracts.*.contract", Description: "Contract definition references in bindings", AllowWildcard: true},
	{Path: "data.contracts.*.methods.*", Description: "Method implementation function references in bindings", AllowWildcard: true},
}

func extractDependenciesInternal(data any) []string {
	var deps []string

	if data == nil {
		return nil
	}

	if m, ok := data.(map[string]any); ok {
		for _, pathConfig := range DependencyPaths {
			pathDeps := extractFromPath(m, pathConfig.Path, pathConfig.AllowWildcard)
			if len(pathDeps) > 0 {
				deps = append(deps, pathDeps...)
			}
		}
	} else if arr, ok := data.([]any); ok {
		for _, item := range arr {
			itemDeps := extractDependenciesInternal(item)
			deps = append(deps, itemDeps...)
		}
	}
	return deps
}

func extractFromPath(data map[string]any, path string, allowWildcard bool) []string {
	segments := strings.Split(path, ".")
	if len(segments) == 0 {
		return nil
	}
	return navigatePath(data, segments, 0, allowWildcard)
}

func navigatePath(currentData any, segments []string, index int, allowWildcard bool) []string {
	if index >= len(segments) {
		return processLeafValue(currentData)
	}

	segment := segments[index]

	if segment == "*" && allowWildcard {
		var deps []string
		if currentMap, ok := currentData.(map[string]any); ok {
			if index >= len(segments)-1 {
				for _, value := range currentMap {
					deps = append(deps, processLeafValue(value)...)
				}
			} else {
				for _, value := range currentMap {
					valueDeps := navigatePath(value, segments, index+1, allowWildcard)
					deps = append(deps, valueDeps...)
				}
			}
		} else if currentArray, ok := currentData.([]any); ok {
			if index >= len(segments)-1 {
				for _, item := range currentArray {
					deps = append(deps, processLeafValue(item)...)
				}
			} else {
				for _, item := range currentArray {
					itemDeps := navigatePath(item, segments, index+1, allowWildcard)
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

	return navigatePath(value, segments, index+1, allowWildcard)
}

func processLeafValue(value any) []string {
	var deps []string
	switch v := value.(type) {
	case string:
		if v != "" {
			deps = append(deps, v)
		}
	case []any:
		extracted := extractStringsFromArray(v)
		deps = append(deps, extracted...)
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

func extractStringsFromArray(arr []any) []string {
	var result []string
	for _, item := range arr {
		if v, ok := item.(string); ok && v != "" {
			result = append(result, v)
		}
	}
	return result
}

func removeDuplicates(slice []string) []string {
	if len(slice) < 2 {
		return slice
	}
	seen := make(map[string]struct{})
	unique := make([]string, 0, len(slice))
	for _, entry := range slice {
		if _, ok := seen[entry]; !ok {
			seen[entry] = struct{}{}
			unique = append(unique, entry)
		}
	}
	return unique
}

func fetchDependencies(entry registry.Entry) []string {
	combinedData := make(map[string]any)

	if len(entry.Meta) > 0 {
		combinedData["meta"] = entry.Meta
	}

	if entry.Data != nil {
		payloadData := entry.Data.Data()
		if payloadData != nil {
			payloadType := reflect.TypeOf(payloadData)
			if payloadType.Kind() == reflect.Map || payloadType.Kind() == reflect.Slice {
				combinedData["data"] = payloadData
			}
		}
	}

	if len(combinedData) == 0 {
		return nil
	}

	rawDeps := extractDependenciesInternal(combinedData)

	if entry.Meta != nil {
		metaTagDeps := entry.Meta.TagValue(registry.TagDependsOn)
		if len(metaTagDeps) > 0 {
			rawDeps = append(rawDeps, metaTagDeps...)
		}
	}

	uniqueDeps := removeDuplicates(rawDeps)
	return uniqueDeps
}
