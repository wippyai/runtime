package stages

import (
	"context"
	"fmt"
	"strings"

	"github.com/ponyruntime/pony/api/boot"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/internal/wildcard"
)

const (
	sectionDisable boot.ConfigSection = "disable"
)

const (
	keyNamespaces = "namespaces"
	keyEntries    = "entries"
)

type disableStage struct{}

// Disable creates a new stage that removes entries based on patterns from boot config.
// Reads from the "disable" config section with two pattern lists:
//   - namespaces: patterns matching entry namespaces
//   - entries: patterns matching full entry IDs (namespace:name format)
//
// Supports wildcard patterns via internal/wildcard:
//   - * matches exactly one segment
//   - ** matches zero or more segments
//   - (a|b|c) matches any of the alternatives
func Disable() boot.Stage {
	return &disableStage{}
}

func (s *disableStage) Name() string {
	return "disable"
}

func (s *disableStage) Execute(ctx context.Context, entries *[]registry.Entry) error {
	cfg := boot.GetConfig(ctx)
	if cfg == nil {
		return nil
	}

	sub := cfg.Sub(string(sectionDisable))

	nsPatterns := readStringSlice(sub, keyNamespaces)
	entryPatterns := readStringSlice(sub, keyEntries)

	if len(nsPatterns) == 0 && len(entryPatterns) == 0 {
		return nil
	}

	nsMatchers, err := compileWildcards(nsPatterns)
	if err != nil {
		return fmt.Errorf("invalid namespace pattern: %w", err)
	}

	entryMatchers, err := compileEntryWildcards(entryPatterns)
	if err != nil {
		return fmt.Errorf("invalid entry pattern: %w", err)
	}

	filtered := make([]registry.Entry, 0, len(*entries))
	for _, e := range *entries {
		if shouldDisable(e, nsMatchers, entryMatchers) {
			continue
		}
		filtered = append(filtered, e)
	}

	*entries = filtered
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
	case []interface{}:
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

// compileWildcards compiles namespace patterns (dot-separated)
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
	nsPattern   string
	namePattern string
	nsMatcher   *wildcard.Wildcard
	nameMatcher *wildcard.Wildcard
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
			return nil, fmt.Errorf("invalid entry pattern '%s': missing ':' separator (expected namespace:name)", pattern)
		}

		nsPattern := pattern[:colonIdx]
		namePattern := pattern[colonIdx+1:]

		if nsPattern == "" {
			return nil, fmt.Errorf("invalid entry pattern '%s': empty namespace", pattern)
		}
		if namePattern == "" {
			return nil, fmt.Errorf("invalid entry pattern '%s': empty name", pattern)
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
func shouldDisable(e registry.Entry, nsMatchers []*wildcard.Wildcard, entryMatchers []*entryWildcard) bool {
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

	return false
}
