// SPDX-License-Identifier: MPL-2.0

package lock

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/wippyai/runtime/api/boot"
)

const (
	workspaceReplacementPrefix        = "replacements."
	workspaceReplacementExcludePrefix = "replacement_excludes."
)

// WithWorkspaceConfig applies the effective .wippy.yaml workspace settings to
// a lock without making them part of the persisted lock data.
func WithWorkspaceConfig(cfg boot.Config) Option {
	return func(l *Lock) error {
		replacements, err := WorkspaceReplacements(cfg)
		if err != nil {
			return err
		}
		l.workspaceOverlay = append([]Replacement(nil), replacements...)
		return nil
	}
}

// WorkspaceReplacements reads the effective workspace replacement map from
// .wippy.yaml after profile application. A null or empty value disables a
// workspace replacement inherited from an earlier config/profile layer.
//
// workspace:
//
//	replacements:
//	  acme/http: ../http
func WorkspaceReplacements(cfg boot.Config) ([]Replacement, error) {
	if cfg == nil {
		return nil, nil
	}
	workspace := cfg.Sub("workspace")
	configDir := cfg.GetString("boot.config_dir", "")
	keys := workspace.Keys()
	sort.Strings(keys)

	replacements := make(map[string]Replacement, len(keys))
	for _, key := range keys {
		if !strings.HasPrefix(key, workspaceReplacementPrefix) {
			continue
		}
		module := strings.TrimPrefix(key, workspaceReplacementPrefix)
		if module == "" {
			return nil, fmt.Errorf("workspace replacement has an empty module name")
		}
		value, ok := workspace.Get(key)
		if !ok || value == nil {
			continue
		}
		path, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("workspace replacement %q path must be a string or null", module)
		}
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		if configDir != "" && !filepath.IsAbs(path) {
			path = filepath.Join(configDir, path)
		}
		replacements[module] = Replacement{From: module, To: path}
	}
	for _, key := range keys {
		if !strings.HasPrefix(key, workspaceReplacementExcludePrefix) {
			continue
		}
		module := strings.TrimPrefix(key, workspaceReplacementExcludePrefix)
		if module == "" {
			return nil, fmt.Errorf("workspace replacement exclude has an empty module name")
		}
		value, ok := workspace.Get(key)
		if !ok || value == nil {
			continue
		}
		exclude, err := replacementExcludePatterns(module, value)
		if err != nil {
			return nil, err
		}
		replacement := replacements[module]
		replacement.From = module
		replacement.Exclude = exclude
		replacements[module] = replacement
	}

	modules := make([]string, 0, len(replacements))
	for module := range replacements {
		modules = append(modules, module)
	}
	sort.Strings(modules)
	result := make([]Replacement, 0, len(modules))
	for _, module := range modules {
		result = append(result, replacements[module])
	}
	return result, nil
}

func replacementExcludePatterns(module string, value any) ([]string, error) {
	values, ok := value.([]any)
	if !ok {
		if strings, ok := value.([]string); ok {
			return append([]string(nil), strings...), nil
		}
		return nil, fmt.Errorf("workspace replacement exclude %q must be a list of entry patterns or null", module)
	}
	patterns := make([]string, len(values))
	for i, value := range values {
		pattern, ok := value.(string)
		if !ok || strings.TrimSpace(pattern) == "" {
			return nil, fmt.Errorf("workspace replacement exclude %q must contain non-empty entry patterns", module)
		}
		patterns[i] = pattern
	}
	return patterns, nil
}
