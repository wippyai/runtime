package stages

import (
	"context"
	"strings"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/internal/wildcard"
	"go.uber.org/zap"
)

const (
	sectionDisable boot.Name = "disable"
)

const (
	keyNamespaces = "namespaces"
	keyEntries    = "entries"
	keyMeta       = "meta"
)

type disableStage struct {
	metaFilters   map[string][]string
	nsPatterns    []string
	entryPatterns []string
}

// DisableOptions configures the Disable stage.
type DisableOptions struct {
	MetaFilters map[string][]string
	Namespaces  []string
	Entries     []string
}

// Disable creates a new stage that removes entries based on patterns.
//
// Supports two modes:
//
//  1. Config mode (default): Reads patterns from boot config "disable" section
//     - disable.namespaces: namespace patterns
//     - disable.entries: entry ID patterns (namespace:name format)
//     - disable.meta: map of meta field to values (entries matching any value are excluded)
//
//  2. CLI mode: Accepts patterns as parameters (overrides config)
//     - nsPatterns: namespace patterns
//     - entryPatterns: entry ID patterns
//
// Supports wildcard patterns via internal/wildcard:
//   - * matches exactly one segment
//   - ** matches zero or more segments
//   - (a|b|c) matches any of the alternatives
//
// Examples:
//
//	Disable()                          // config mode
//	Disable([]string{"test.**"}, nil)  // CLI mode - exclude test namespace
func Disable(patterns ...[]string) boot.Stage {
	stage := &disableStage{}
	if len(patterns) > 0 {
		stage.nsPatterns = patterns[0]
	}
	if len(patterns) > 1 {
		stage.entryPatterns = patterns[1]
	}
	return stage
}

// DisableWithOptions creates a Disable stage with full configuration options.
func DisableWithOptions(opts DisableOptions) boot.Stage {
	return &disableStage{
		nsPatterns:    opts.Namespaces,
		entryPatterns: opts.Entries,
		metaFilters:   opts.MetaFilters,
	}
}

func (s *disableStage) Name() string {
	return "disable"
}

func (s *disableStage) Execute(ctx context.Context, entries *[]registry.Entry) error {
	log := logs.GetLogger(ctx)

	var nsPatterns, entryPatterns []string
	var metaFilters map[string][]string
	var mode string

	if len(s.nsPatterns) > 0 || len(s.entryPatterns) > 0 || len(s.metaFilters) > 0 {
		nsPatterns = s.nsPatterns
		entryPatterns = s.entryPatterns
		metaFilters = s.metaFilters
		mode = "CLI"
	} else {
		cfg := boot.GetConfig(ctx)
		if cfg != nil {
			sub := cfg.Sub(sectionDisable)
			nsPatterns = readStringSlice(sub, keyNamespaces)
			entryPatterns = readStringSlice(sub, keyEntries)
			metaFilters = readMetaFilters(sub, keyMeta)
			mode = "config"
		}
	}

	if len(nsPatterns) == 0 && len(entryPatterns) == 0 && len(metaFilters) == 0 {
		return nil
	}

	nsMatchers, err := compileWildcards(nsPatterns)
	if err != nil {
		return NewInvalidNamespacePatternError(err)
	}

	entryMatchers, err := compileEntryWildcards(entryPatterns)
	if err != nil {
		return NewInvalidEntryPatternError(err)
	}

	originalCount := len(*entries)
	filtered := make([]registry.Entry, 0, len(*entries))

	for _, e := range *entries {
		if shouldDisable(e, nsMatchers, entryMatchers, metaFilters) {
			log.Debug("disabled entry",
				zap.String("id", e.ID.String()),
				zap.String("kind", e.Kind))
			continue
		}
		filtered = append(filtered, e)
	}

	*entries = filtered
	disabledCount := originalCount - len(filtered)

	if disabledCount > 0 {
		log.Info("disabled entries",
			zap.String("source", mode),
			zap.Int("disabled", disabledCount),
			zap.Int("remaining", len(filtered)),
			zap.Int("ns_patterns", len(nsPatterns)),
			zap.Int("entry_patterns", len(entryPatterns)),
			zap.Int("meta_filters", len(metaFilters)))
	}

	return nil
}

// readStringSlice reads a string slice from config
func readStringSlice(cfg boot.Config, key string) []string {
	val, ok := cfg.Get(key)
	if !ok {
		return nil
	}

	switch v := val.(type) {
	case []string:
		return v
	case []any:
		result := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
		return result
	default:
		return nil
	}
}

// readMetaFilters reads meta filter map from config
func readMetaFilters(cfg boot.Config, key string) map[string][]string {
	val, ok := cfg.Get(key)
	if !ok {
		return nil
	}

	result := make(map[string][]string)

	switch v := val.(type) {
	case map[string][]string:
		return v
	case map[string]any:
		for k, values := range v {
			switch vals := values.(type) {
			case []string:
				result[k] = vals
			case []any:
				strs := make([]string, 0, len(vals))
				for _, item := range vals {
					if s, ok := item.(string); ok {
						strs = append(strs, s)
					}
				}
				if len(strs) > 0 {
					result[k] = strs
				}
			case string:
				result[k] = []string{vals}
			}
		}
	case map[any]any:
		for k, values := range v {
			key, ok := k.(string)
			if !ok {
				continue
			}
			switch vals := values.(type) {
			case []string:
				result[key] = vals
			case []any:
				strs := make([]string, 0, len(vals))
				for _, item := range vals {
					if s, ok := item.(string); ok {
						strs = append(strs, s)
					}
				}
				if len(strs) > 0 {
					result[key] = strs
				}
			case string:
				result[key] = []string{vals}
			}
		}
	}

	if len(result) == 0 {
		return nil
	}
	return result
}

// compileWildcards compiles namespace patterns (dot-separated)
//
//nolint:unparam // error return reserved for future validation
func compileWildcards(patterns []string) ([]*wildcard.Wildcard, error) {
	matchers := make([]*wildcard.Wildcard, 0, len(patterns))
	for _, pattern := range patterns {
		if pattern == "" {
			continue
		}
		matchers = append(matchers, wildcard.NewWildcard(pattern))
	}
	return matchers, nil
}

// entryWildcard wraps wildcard for entry ID matching with colon separator
type entryWildcard struct {
	nsMatcher   *wildcard.Wildcard
	nameMatcher *wildcard.Wildcard
	nsPattern   string
	namePattern string
}

// compileEntryWildcards compiles entry ID patterns (namespace:name format)
func compileEntryWildcards(patterns []string) ([]*entryWildcard, error) {
	matchers := make([]*entryWildcard, 0, len(patterns))
	for _, pattern := range patterns {
		if pattern == "" {
			continue
		}

		colonIdx := strings.Index(pattern, ":")
		if colonIdx == -1 {
			return nil, NewInvalidEntryPatternFormatError(pattern, "missing ':' separator (expected namespace:name)")
		}

		nsPattern := pattern[:colonIdx]
		namePattern := pattern[colonIdx+1:]

		if nsPattern == "" {
			return nil, NewInvalidEntryPatternFormatError(pattern, "empty namespace")
		}
		if namePattern == "" {
			return nil, NewInvalidEntryPatternFormatError(pattern, "empty name")
		}

		matchers = append(matchers, &entryWildcard{
			nsPattern:   nsPattern,
			namePattern: namePattern,
			nsMatcher:   wildcard.NewWildcard(nsPattern),
			nameMatcher: wildcard.NewWildcard(namePattern),
		})
	}
	return matchers, nil
}

// shouldDisable checks if an entry matches any disable pattern
func shouldDisable(e registry.Entry, nsMatchers []*wildcard.Wildcard, entryMatchers []*entryWildcard, metaFilters map[string][]string) bool {
	for _, m := range nsMatchers {
		if m.Match(e.ID.NS) {
			return true
		}
	}

	for _, m := range entryMatchers {
		if m.nsMatcher.Match(e.ID.NS) && m.nameMatcher.Match(e.ID.Name) {
			return true
		}
	}

	return matchesMetaFilter(e.Meta, metaFilters)
}

// matchesMetaFilter checks if entry meta matches any filter
func matchesMetaFilter(meta map[string]any, filters map[string][]string) bool {
	if len(filters) == 0 || meta == nil {
		return false
	}

	for key, values := range filters {
		metaVal, ok := meta[key]
		if !ok {
			continue
		}

		metaStr, ok := metaVal.(string)
		if !ok {
			continue
		}

		for _, v := range values {
			if metaStr == v {
				return true
			}
		}
	}

	return false
}
