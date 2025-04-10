package topology

// Fields that directly contain dependency references
var directDepFields = []string{
	"server",      // Reference to HTTP server
	"fs",          // Reference to filesystem
	"store",       // Reference to a store (e.g., "session")
	"token_store", // Reference to token storage (e.g., "app.security:tokens")
	"set",         // Reference to a template set
}

// Fields that contain arrays of dependencies
var arrayDepFields = []string{
	"middleware",      // List of middleware components
	"post_middleware", // List of post-middleware components
}

// RegisterYAMLDependencyHandler registers a handler for YAML-based configurations
func init() {
	RegisterDepFetcherHandler(func(data any) []string {
		return extractYAMLDependencies(data)
	})
}

// extractYAMLDependencies extracts dependencies from YAML-based configuration data
func extractYAMLDependencies(data any) []string {
	var deps []string

	// Process map-like structures
	if m, ok := data.(map[string]interface{}); ok {
		deps = append(deps, extractFromMap(m)...)
	}

	// Handle array of maps case
	if arr, ok := data.([]interface{}); ok {
		for _, item := range arr {
			if m, ok := item.(map[string]interface{}); ok {
				deps = append(deps, extractFromMap(m)...)
			}
		}
	}

	return deps
}

// extractFromMap extracts dependencies from a map structure
func extractFromMap(m map[string]interface{}) []string {
	var deps []string

	// Note: meta.depends_on is already processed elsewhere, so we skip it

	// Check for meta.router and meta.server
	if meta, ok := m["meta"].(map[string]interface{}); ok {
		if router, ok := meta["router"].(string); ok && router != "" {
			deps = append(deps, router)
		}
		if server, ok := meta["server"].(string); ok && server != "" {
			deps = append(deps, server)
		}
	}

	// Check for direct dependency fields
	for _, field := range directDepFields {
		if value, ok := m[field]; ok && value != nil {
			if strValue, ok := value.(string); ok && strValue != "" {
				deps = append(deps, strValue)
			}
		}
	}

	// Check for array dependency fields
	for _, field := range arrayDepFields {
		if value, ok := m[field]; ok && value != nil {
			deps = append(deps, extractDepsFromValue(value)...)
		}
	}

	// Check for imports - we only want the values, not the keys
	if imports, ok := m["imports"].(map[string]interface{}); ok {
		for _, value := range imports {
			if strValue, ok := value.(string); ok && strValue != "" {
				deps = append(deps, strValue)
			}
		}
	}

	// Recursively process nested maps
	for _, value := range m {
		switch v := value.(type) {
		case map[string]interface{}:
			deps = append(deps, extractFromMap(v)...)
		case []interface{}:
			for _, item := range v {
				if itemMap, ok := item.(map[string]interface{}); ok {
					deps = append(deps, extractFromMap(itemMap)...)
				}
			}
		}
	}

	return deps
}

// extractDepsFromValue handles various types of dependency values
func extractDepsFromValue(value interface{}) []string {
	var deps []string

	switch v := value.(type) {
	case string:
		if v != "" {
			deps = append(deps, v)
		}
	case []interface{}:
		for _, item := range v {
			if strItem, ok := item.(string); ok && strItem != "" {
				deps = append(deps, strItem)
			} else if mapItem, ok := item.(map[string]interface{}); ok {
				deps = append(deps, extractFromMap(mapItem)...)
			}
		}
	case []string:
		for _, item := range v {
			if item != "" {
				deps = append(deps, item)
			}
		}
	case map[string]interface{}:
		deps = append(deps, extractFromMap(v)...)
	}

	return deps
}
