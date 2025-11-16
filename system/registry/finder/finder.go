package finder

import (
	"fmt"
	"hash/fnv"
	"regexp"
	"sort"
	"strings"
	"sync/atomic"

	"go.uber.org/zap"

	"github.com/wippyai/runtime/api/registry"
	lru "github.com/wippyai/runtime/internal/cache"
)

const (
	// Default cache limits
	defaultQueryCacheSize = 1000 // Max number of cached query results
	defaultRegexCacheSize = 100  // Max number of compiled regex patterns
)

// Option defines functional options for Finder
type Option func(*memoryFinder)

// WithQueryCacheSize sets the maximum number of cached query results
func WithQueryCacheSize(size int) Option {
	return func(f *memoryFinder) {
		f.queryCache = lru.New[queryCacheKey, queryResult](lru.WithCapacity(size))
	}
}

// WithRegexCacheSize sets the maximum number of compiled regex patterns to cache
func WithRegexCacheSize(size int) Option {
	return func(f *memoryFinder) {
		f.regexCache = lru.New[string, *regexp.Regexp](lru.WithCapacity(size))
	}
}

// memoryFinder implements the Finder interface for in-memory registry state with version-aware caching
type memoryFinder struct {
	reg registry.EntryReader
	log *zap.Logger

	// Version-aware caching with bounded LRU
	lastVersion atomic.Uint64                          // Last seen version ID
	queryCache  *lru.Cache[queryCacheKey, queryResult] // Bounded query result cache
	regexCache  *lru.Cache[string, *regexp.Regexp]     // Bounded regex cache - shared across forks
}

type queryCacheKey struct {
	versionID uint64
	queryHash uint64
}

type queryResult struct {
	entries []registry.Entry
}

// NewFinder creates a new Finder that can search registry entries with bounded LRU caching.
// Use Option functions to configure cache sizes.
func NewFinder(r registry.EntryReader, log *zap.Logger, opts ...Option) registry.Finder {
	if log == nil {
		log = zap.NewNop()
	}
	f := &memoryFinder{
		reg:        r,
		log:        log,
		queryCache: lru.New[queryCacheKey, queryResult](lru.WithCapacity(defaultQueryCacheSize)),
		regexCache: lru.New[string, *regexp.Regexp](lru.WithCapacity(defaultRegexCacheSize)),
	}

	// Apply options
	for _, opt := range opts {
		opt(f)
	}

	return f
}

// Fork creates a new Finder for a different EntryReader (e.g., snapshot) that shares the regex cache
// from the source finder. This is useful for snapshot finders that are created frequently but can
// benefit from shared compiled regex patterns.
func Fork(source registry.Finder, r registry.EntryReader, log *zap.Logger) registry.Finder {
	// Try to extract regex cache from source if it's a memoryFinder
	if mf, ok := source.(*memoryFinder); ok {
		if log == nil {
			log = mf.log
		}
		return &memoryFinder{
			reg:        r,
			log:        log,
			queryCache: lru.New[queryCacheKey, queryResult](lru.WithCapacity(defaultQueryCacheSize)),
			regexCache: mf.regexCache, // Share regex cache
		}
	}

	// Fallback: create new finder if source is not a memoryFinder
	return NewFinder(r, log)
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
// Metadata field matching operators (meta. prefix required):
//   - "meta.field": Standard equality match for the field
//   - "~meta.field": Regex pattern match (e.g., "~meta.description": ".*service.*")
//   - "*meta.field": Contains match (substring search)
//   - "^meta.field": Prefix match (starts with)
//   - "$meta.field": Suffix match (ends with)
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
	// Get current version from registry
	var currentVersion uint64
	if versionedReg, ok := f.reg.(interface {
		Current() (registry.Version, error)
	}); ok {
		v, err := versionedReg.Current()
		if err == nil && v != nil {
			currentVersion = uint64(v.ID())
		}
	}

	// Check version change - invalidate cache if changed
	lastVer := f.lastVersion.Load()
	if currentVersion != lastVer && currentVersion > 0 {
		f.lastVersion.Store(currentVersion)
		if lastVer > 0 {
			// Version changed - create new query cache (keep regex cache)
			f.queryCache = lru.New[queryCacheKey, queryResult](lru.WithCapacity(defaultQueryCacheSize))
			f.log.Debug("finder cache invalidated due to version change",
				zap.Uint64("old_version", lastVer),
				zap.Uint64("new_version", currentVersion))
		}
	}

	// Generate cache key
	queryHash := hashMetadata(meta)
	cacheKey := queryCacheKey{
		versionID: currentVersion,
		queryHash: queryHash,
	}

	// Try cache lookup
	if cached, ok := f.queryCache.Get(cacheKey); ok {
		return cached.entries, nil
	}

	// Cache miss - execute search
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
		// Handle root field matchers first (they start with ".")
		if strings.HasPrefix(key, ".") {
			rootField := key[1:] // Remove the dot
			rootMatchers[rootField] = value
			continue
		}

		// Extract operator prefix and field name
		var operatorPrefix string
		var fieldWithMeta string

		switch {
		case strings.HasPrefix(key, "~"):
			operatorPrefix = "~"
			fieldWithMeta = key[1:]
		case strings.HasPrefix(key, "*"):
			operatorPrefix = "*"
			fieldWithMeta = key[1:]
		case strings.HasPrefix(key, "^"):
			operatorPrefix = "^"
			fieldWithMeta = key[1:]
		case strings.HasPrefix(key, "$"):
			operatorPrefix = "$"
			fieldWithMeta = key[1:]
		default:
			operatorPrefix = ""
			fieldWithMeta = key
		}

		// Check for and strip "meta." prefix (required in v2)
		var finalField string
		if strings.HasPrefix(fieldWithMeta, "meta.") {
			finalField = fieldWithMeta[5:] // Remove "meta." prefix
		} else {
			// V2: meta. prefix is required
			f.log.Warn("metadata field must use 'meta.' prefix",
				zap.String("field", key),
				zap.String("use_instead", "meta."+fieldWithMeta))
			continue // Skip fields without meta. prefix
		}

		// Process based on operator type
		switch operatorPrefix {
		case "~":
			// Handle regex matchers with caching
			if strVal, ok := value.(string); ok {
				// Check regex cache first
				if cached, found := f.regexCache.Get(strVal); found {
					regexMatchers[finalField] = cached
				} else {
					regex, err := regexp.Compile(strVal)
					if err != nil {
						f.log.Warn("invalid regex pattern",
							zap.String("field", key),
							zap.String("pattern", strVal),
							zap.Error(err))
						continue
					}
					f.regexCache.Set(strVal, regex)
					regexMatchers[finalField] = regex
				}
			}

		case "*":
			// Handle contains matchers
			if strVal, ok := value.(string); ok {
				containsMatchers[finalField] = strVal
			}

		case "^":
			// Handle prefix matchers
			if strVal, ok := value.(string); ok {
				prefixMatchers[finalField] = strVal
			}

		case "$":
			// Handle suffix matchers
			if strVal, ok := value.(string); ok {
				suffixMatchers[finalField] = strVal
			}

		default:
			// Standard metadata matching
			standardMeta[finalField] = value
		}
	}

	// Filter entries
	result := make([]registry.Entry, 0)
	for _, entry := range entries {
		if matchesAllCriteria(entry, rootMatchers, regexMatchers, containsMatchers,
			prefixMatchers, suffixMatchers, standardMeta) {
			result = append(result, entry)
		}
	}

	// Cache result
	f.queryCache.Set(cacheKey, queryResult{entries: result})

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
	standardMeta registry.Metadata,
) bool {
	// Check root field matchers
	for field, value := range rootMatchers {
		switch field {
		case "kind":
			if strVal, ok := value.(string); ok && entry.Kind != strVal {
				return false
			}
		case "name":
			if strVal, ok := value.(string); ok && entry.ID.Name != strVal {
				return false
			}
		case "ns":
			if strVal, ok := value.(string); ok && entry.ID.NS != strVal {
				return false
			}
		case "id":
			fullID := entry.ID.NS + ":" + entry.ID.Name
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
		if !matchContains(entry.Meta[field], substr) {
			return false
		}
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

	// Check standard metadata (equality matching)
	for key, expectedValue := range standardMeta {
		actualValue, exists := entry.Meta[key]
		if !exists {
			return false
		}

		// Handle array matching (tags) - ALL expected tags must be present (AND logic)
		if expectedArray, ok := expectedValue.([]string); ok {
			if !matchArrayContainsAll(actualValue, expectedArray) {
				return false
			}
			continue
		}

		// Simple equality for primitives
		if actualValue != expectedValue {
			return false
		}
	}

	return true
}

// matchContains checks if a value contains a substring (handles strings and arrays)
func matchContains(value interface{}, substr string) bool {
	// Handle string fields
	if strValue, ok := value.(string); ok {
		return strings.Contains(strValue, substr)
	}

	// Handle string array fields (tags)
	if strArray, ok := value.([]string); ok {
		for _, tag := range strArray {
			if strings.Contains(tag, substr) {
				return true
			}
		}
		return false
	}

	// Handle []interface{} fields (from JSON unmarshaling)
	if anyArray, ok := value.([]interface{}); ok {
		for _, anyVal := range anyArray {
			if strVal, ok := anyVal.(string); ok && strings.Contains(strVal, substr) {
				return true
			}
		}
		return false
	}

	return false
}

// matchArrayContainsAll checks if actual array contains all expected elements
func matchArrayContainsAll(actual interface{}, expected []string) bool {
	// Handle []string
	if actualArray, ok := actual.([]string); ok {
		for _, expectedTag := range expected {
			found := false
			for _, actualTag := range actualArray {
				if actualTag == expectedTag {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
		return true
	}

	// Handle []interface{} (from JSON)
	if actualAnyArray, ok := actual.([]interface{}); ok {
		for _, expectedTag := range expected {
			found := false
			for _, anyVal := range actualAnyArray {
				if strVal, ok := anyVal.(string); ok && strVal == expectedTag {
					found = true
					break
				}
			}
			if !found {
				return false
			}
		}
		return true
	}

	return false
}

// hashMetadata generates a consistent hash for metadata search criteria
func hashMetadata(meta registry.Metadata) uint64 {
	h := fnv.New64a()

	// Sort keys for consistent hashing
	keys := make([]string, 0, len(meta))
	for k := range meta {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// Hash sorted key-value pairs
	for _, k := range keys {
		_, _ = h.Write([]byte(k))
		_, _ = h.Write([]byte(":"))

		// Handle different value types for consistent hashing
		switch v := meta[k].(type) {
		case string:
			_, _ = h.Write([]byte(v))
		case []string:
			for _, s := range v {
				_, _ = h.Write([]byte(s))
				_, _ = h.Write([]byte(","))
			}
		default:
			// Fallback to string representation
			_, _ = fmt.Fprintf(h, "%v", v)
		}
	}

	return h.Sum64()
}
