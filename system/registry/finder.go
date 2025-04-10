package registry

import (
	"log"
	"regexp"
	"strings"

	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/internal/metamatch"
)

// memoryFinder implements the Finder interface for in-memory registry state
type memoryFinder struct {
	reg registry.EntryReader
}

// NewFinder creates a new Finder that can search registry entries
func NewFinder(r registry.EntryReader) registry.Finder {
	return &memoryFinder{reg: r}
}

// Find retrieves all entries matching the provided search criteria.
//
// The search criteria supports various operators through field name prefixes:
//
// Root fields (special prefixes):
//   - ".kind": Match entry's Kind field (exact match)
//   - ".name": Match entry's ID.Name field (exact match)
//   - ".ns": Match entry's ID.Namespace field (exact match)
//   - ".id": Match entry's full ID (exact match)
//
// Metadata field matching operators:
//   - "field" or "meta.field": Standard equality match for the field
//   - "~field" or "~meta.field": Regex pattern match (e.g., "~description": ".*service.*")
//   - "*field" or "*meta.field": Contains match (substring search)
//   - "^field" or "^meta.field": Prefix match (starts with)
//   - "$field" or "$meta.field": Suffix match (ends with)
//
// Examples:
//
//	Find({".kind": "service", "meta.enabled": true})
//	  -> Find all services with enabled=true
//
//	Find({".ns": "app.tools", "meta.type": "tool"})
//	  -> Find all tools in the app.tools namespace
//
//	Find({"~meta.description": ".*api.*", "*meta.tags": "backend"})
//	  -> Find entries with description matching regex ".*api.*" and tags containing "backend"
func (f *memoryFinder) Find(meta registry.Metadata) ([]registry.Entry, error) {
	// Get all entries
	entries, err := f.reg.GetAllEntries()
	if err != nil {
		return nil, err
	}

	// Extract special fields and create matchers
	rootMatchers := make(map[string]interface{})
	regexMatchers := make(map[string]*regexp.Regexp)
	containsMatchers := make(map[string]string)
	prefixMatchers := make(map[string]string)
	suffixMatchers := make(map[string]string)

	// Regular metadata for standard matching
	standardMeta := make(registry.Metadata)

	// Process search criteria
	for key, value := range meta {
		// Strip 'meta.' prefix if present and log deprecated usage without prefix
		var originalKey = key
		var actualField = key
		var isMetaPrefixed = false

		if strings.HasPrefix(key, "meta.") {
			actualField = key[5:] // Remove the 'meta.' prefix
			isMetaPrefixed = true
		}

		// Handle root field matchers
		if strings.HasPrefix(key, ".") {
			rootField := key[1:] // Remove the dot
			rootMatchers[rootField] = value
			continue
		}

		// Handle regex matchers
		if strings.HasPrefix(actualField, "~") {
			field := actualField[1:] // Remove the ~ prefix
			if strVal, ok := value.(string); ok {
				regex, err := regexp.Compile(strVal)
				if err == nil {
					regexMatchers[field] = regex
				}
			}
			if !isMetaPrefixed && !strings.HasPrefix(originalKey, "meta.") {
				log.Printf("Deprecated: Using '~%s' without 'meta.' prefix. Consider using '~meta.%s' instead.", field, field)
			}
			continue
		}

		// Handle contains matchers
		if strings.HasPrefix(actualField, "*") {
			field := actualField[1:] // Remove the * prefix
			if strVal, ok := value.(string); ok {
				containsMatchers[field] = strVal
			}
			if !isMetaPrefixed && !strings.HasPrefix(originalKey, "meta.") {
				log.Printf("Deprecated: Using '*%s' without 'meta.' prefix. Consider using '*meta.%s' instead.", field, field)
			}
			continue
		}

		// Handle prefix matchers
		if strings.HasPrefix(actualField, "^") {
			field := actualField[1:] // Remove the ^ prefix
			if strVal, ok := value.(string); ok {
				prefixMatchers[field] = strVal
			}
			if !isMetaPrefixed && !strings.HasPrefix(originalKey, "meta.") {
				log.Printf("Deprecated: Using '^%s' without 'meta.' prefix. Consider using '^meta.%s' instead.", field, field)
			}
			continue
		}

		// Handle suffix matchers
		if strings.HasPrefix(actualField, "$") {
			field := actualField[1:] // Remove the $ prefix
			if strVal, ok := value.(string); ok {
				suffixMatchers[field] = strVal
			}
			if !isMetaPrefixed && !strings.HasPrefix(originalKey, "meta.") {
				log.Printf("Deprecated: Using '$%s' without 'meta.' prefix. Consider using '$meta.%s' instead.", field, field)
			}
			continue
		}

		// Standard metadata matching
		if isMetaPrefixed {
			standardMeta[actualField] = value
		} else {
			standardMeta[actualField] = value
			log.Printf("Deprecated: Using metadata field '%s' without 'meta.' prefix. Consider using 'meta.%s' instead.", actualField, actualField)
		}
	}

	// Create standard matcher
	standardMatcher := metadataToMatcher(standardMeta)

	// Filter entries
	var result []registry.Entry
	for _, entry := range entries {
		// Check if entry should be included
		if !matchesAllCriteria(entry, rootMatchers, regexMatchers, containsMatchers,
			prefixMatchers, suffixMatchers, standardMatcher) {
			continue
		}

		// All criteria matched, include this entry
		result = append(result, entry)
	}

	return result, nil
}

// matchesAllCriteria checks if an entry matches all the search criteria
func matchesAllCriteria(
	entry registry.Entry,
	rootMatchers map[string]interface{},
	regexMatchers map[string]*regexp.Regexp,
	containsMatchers map[string]string,
	prefixMatchers map[string]string,
	suffixMatchers map[string]string,
	standardMatcher *metamatch.Matcher,
) bool {
	// Check root field matchers
	for field, value := range rootMatchers {
		switch field {
		case "kind":
			if strVal, ok := value.(string); ok && entry.Kind != strVal {
				return false
			}
		case "name":
			if strVal, ok := value.(string); ok && string(entry.ID.Name) != strVal {
				return false
			}
		case "ns":
			if strVal, ok := value.(string); ok && string(entry.ID.NS) != strVal {
				return false
			}
		case "id":
			// Match the full ID string
			fullID := string(entry.ID.NS) + ":" + string(entry.ID.Name)
			if strVal, ok := value.(string); ok && fullID != strVal {
				return false
			}
		}
	}

	// Check regex matchers
	for field, regex := range regexMatchers {
		metaValue, isString := entry.Meta[field].(string)
		if !isString || !regex.MatchString(metaValue) {
			return false
		}
	}

	// Check contains matchers
	for field, substr := range containsMatchers {
		// Handle string fields
		if metaValue, isString := entry.Meta[field].(string); isString {
			if !strings.Contains(metaValue, substr) {
				return false
			}
			continue
		}

		// Handle string array fields (tags)
		if tags, ok := entry.Meta[field].([]string); ok {
			found := false
			for _, tag := range tags {
				if strings.Contains(tag, substr) {
					found = true
					break
				}
			}
			if !found {
				return false
			}
			continue
		}

		// Handle []any fields that might contain strings
		if anyTags, ok := entry.Meta[field].([]interface{}); ok {
			found := false
			for _, anyTag := range anyTags {
				if tag, isString := anyTag.(string); isString {
					if strings.Contains(tag, substr) {
						found = true
						break
					}
				}
			}
			if !found {
				return false
			}
			continue
		}

		// If we reach here, we couldn't find a match
		return false
	}

	// Check prefix matchers
	for field, prefix := range prefixMatchers {
		metaValue, isString := entry.Meta[field].(string)
		if !isString || !strings.HasPrefix(metaValue, prefix) {
			return false
		}
	}

	// Check suffix matchers
	for field, suffix := range suffixMatchers {
		metaValue, isString := entry.Meta[field].(string)
		if !isString || !strings.HasSuffix(metaValue, suffix) {
			return false
		}
	}

	// Check standard metadata matcher
	if !standardMatcher.Match(entry.Meta) {
		return false
	}

	return true
}

// metadataToMatcher converts registry metadata to a metamatch.Matcher
func metadataToMatcher(metadata registry.Metadata) *metamatch.Matcher {
	matcher := metamatch.NewMatcher()

	// Add conditions for each metadata entry
	for key, value := range metadata {
		switch v := value.(type) {
		case string:
			matcher = matcher.WithStringValue(key, v)
		case bool:
			matcher = matcher.WithBoolValue(key, v)
		case int:
			matcher = matcher.WithIntValue(key, v)
		case []string:
			// For string arrays, we need to match any of the provided tags (OR logic for each tag in search criteria)
			for _, tag := range v {
				matcher = matcher.WithTagContains(key, tag)
			}
		default:
			// For other types, use exact value matching
			matcher = matcher.WithExactValue(key, value)
		}
	}

	return matcher
}
