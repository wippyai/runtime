package dependsadjuster

import (
	"strings"

	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	"go.uber.org/zap"
)

// todo: move it closer to topology dep
func NewAdjuster(logger *zap.Logger) *Adjuster {
	return &Adjuster{
		logger: logger,
	}
}

type Adjuster struct {
	logger *zap.Logger
}

func (a *Adjuster) Adjust(entries []registry.Entry) ([]registry.Entry, error) {
	for i := range entries {
		entry := &entries[i]

		// Get the data from the payload
		var data map[string]any
		if entry.Data != nil {
			if rawData, ok := entry.Data.Data().(map[string]any); ok {
				data = rawData
			} else {
				// If data is not a map, skip this entry
				continue
			}
		} else {
			data = make(map[string]any)
		}

		// Extract environment dependencies from the entry data
		envDeps := a.extractEnvPatternDependencies(data)

		// Add environment dependencies to depends_on slice
		if len(envDeps) > 0 {
			// Get existing depends_on or create new slice
			var dependsOn []string
			if existing, ok := data["depends_on"]; ok {
				if deps, ok := existing.([]string); ok {
					dependsOn = deps
				} else if deps, ok := existing.([]any); ok {
					// Convert []any to []string
					for _, dep := range deps {
						if str, ok := dep.(string); ok {
							dependsOn = append(dependsOn, str)
						}
					}
				}
			}

			// Add new environment dependencies
			dependsOn = append(dependsOn, envDeps...)
			data["depends_on"] = dependsOn

			// Create new payload with updated data
			entry.Data = payload.New(data)
		}
	}

	return entries, nil
}

// extractEnvPatternDependencies extracts dependencies from fields ending with "_env"
func (a *Adjuster) extractEnvPatternDependencies(data map[string]any) []string {
	var deps []string

	// Recursively search for *_env patterns in the data structure
	a.extractEnvPatternFromValue(data, &deps)

	return deps
}

// extractEnvPatternFromValue recursively searches for *_env patterns in any value
func (a *Adjuster) extractEnvPatternFromValue(value any, deps *[]string) {
	switch v := value.(type) {
	case map[string]any:
		for key, val := range v {
			// Check if the key ends with "_env"
			if strings.HasSuffix(key, "_env") {
				// Verify that value has type string and return without processing if not
				if strVal, ok := val.(string); ok {
					a.extractDepsFromEnvValue(strVal, deps)
				}
			} else {
				// Recursively search in nested structures
				a.extractEnvPatternFromValue(val, deps)
			}
		}
	case []any:
		for _, item := range v {
			a.extractEnvPatternFromValue(item, deps)
		}
	}
}

// extractDepsFromEnvValue extracts dependencies from environment variable values
func (a *Adjuster) extractDepsFromEnvValue(value any, deps *[]string) {
	switch v := value.(type) {
	case string:
		if v != "" {
			nsName := a.extractNsName(v)
			if nsName != "" {
				*deps = append(*deps, nsName)
			}
		}
	case []any:
		for _, item := range v {
			if str, ok := item.(string); ok && str != "" {
				nsName := a.extractNsName(str)
				if nsName != "" {
					*deps = append(*deps, nsName)
				}
			}
		}
	case []string:
		for _, s := range v {
			if s != "" {
				nsName := a.extractNsName(s)
				if nsName != "" {
					*deps = append(*deps, nsName)
				}
			}
		}
	}
}

// extractNsName extracts the ns:name part from an environment value
func (a *Adjuster) extractNsName(envValue string) string {
	// Split by colon to get all parts
	parts := strings.Split(envValue, ":")
	if len(parts) >= 2 {
		ns := strings.TrimSpace(parts[0])
		name := strings.TrimSpace(parts[1])
		if ns != "" && name != "" {
			return ns + ":" + name
		}
	}
	return ""
}
