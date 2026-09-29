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
	workspaceReplacementPrefix = "replacements."
	workspaceUnpackModulesKey  = "options.unpack_modules"
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
		unpack, err := WorkspaceUnpackModules(cfg)
		if err != nil {
			return err
		}
		l.unpackOverride = unpack
		return nil
	}
}

// WorkspaceUnpackModules reads options.unpack_modules from the effective
// runtime config, including --set overrides. Nil means the lock file value
// applies.
func WorkspaceUnpackModules(cfg boot.Config) (*bool, error) {
	if cfg == nil {
		return nil, nil
	}
	raw, ok := cfg.Get(workspaceUnpackModulesKey)
	if !ok || raw == nil {
		return nil, nil
	}
	unpack, ok := raw.(bool)
	if !ok {
		return nil, fmt.Errorf("%s must be a boolean, got %T", workspaceUnpackModulesKey, raw)
	}
	return &unpack, nil
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

	replacements := make([]Replacement, 0, len(keys))
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
		replacements = append(replacements, Replacement{From: module, To: path})
	}
	return replacements, nil
}
