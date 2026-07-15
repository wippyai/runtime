// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/boot/deps/config"
	"github.com/wippyai/runtime/cmd/internal/bootconfig"
)

const (
	publishRuntimeMetadataKey         = "runtime"
	publishRuntimeProfilesMetadataKey = "profiles"
	publishRuntimeVarsMetadataKey     = "vars"
)

func addPublishedRuntimeProfileMetadata(metadata attrs.Bag, configDir string, profileCfg config.PublishProfilesConfig) error {
	if hasNestedRuntimeMetadata(metadata, publishRuntimeProfilesMetadataKey) {
		return fmt.Errorf("wippy.yaml metadata.runtime.profiles is not supported; declare publishable profiles in .wippy.yaml or publish.profiles.source")
	}

	if profileCfg.Enabled != nil && !*profileCfg.Enabled {
		return nil
	}

	source := strings.TrimSpace(profileCfg.Source)
	if source == "" {
		source = defaultConfigFile
	}
	if !filepath.IsAbs(source) {
		source = filepath.Join(configDir, source)
	}

	cfg, err := bootconfig.Load(source)
	if err != nil {
		return fmt.Errorf("load runtime profile source %s: %w", source, err)
	}
	if cfg == nil {
		return nil
	}

	profiles, err := runtimeProfilesFromConfig(cfg, profileCfg.Include)
	if err != nil {
		return err
	}
	if len(profiles) == 0 {
		return nil
	}

	if hasNestedRuntimeMetadata(metadata, publishRuntimeVarsMetadataKey) {
		return fmt.Errorf("runtime vars are defined in both %s and wippy.yaml metadata; keep profile variables in the profile source", source)
	}

	runtime, err := runtimeMetadataMap(metadata)
	if err != nil {
		return err
	}
	runtime[publishRuntimeProfilesMetadataKey] = profiles

	vars := runtimeSectionFromConfig(cfg, publishRuntimeVarsMetadataKey)
	if len(vars) > 0 {
		runtime[publishRuntimeVarsMetadataKey] = vars
	}

	return nil
}

func runtimeProfilesFromConfig(cfg boot.Config, include []string) (map[string]any, error) {
	allProfiles := make(map[string]any)
	if cfg == nil {
		return allProfiles, nil
	}

	for _, key := range cfg.Keys() {
		if !strings.HasPrefix(key, "profiles.") {
			continue
		}

		rest := strings.TrimPrefix(key, "profiles.")
		profileName, rest, ok := strings.Cut(rest, ".")
		if !ok || profileName == "" || rest == "" {
			return nil, fmt.Errorf("invalid runtime profile key %q", key)
		}
		section, subkey, ok := strings.Cut(rest, ".")
		if !ok || section == "" || subkey == "" {
			return nil, fmt.Errorf("invalid runtime profile key %q", key)
		}
		// Workspace configuration is machine-local by definition. It must never
		// enter package metadata, even when the containing profile is published.
		if section == "workspace" {
			continue
		}

		profileMap, ok := allProfiles[profileName].(map[string]any)
		if !ok {
			profileMap = make(map[string]any)
			allProfiles[profileName] = profileMap
		}

		sectionMap, ok := profileMap[section].(map[string]any)
		if !ok {
			sectionMap = make(map[string]any)
			profileMap[section] = sectionMap
		}

		value, _ := cfg.Get(key)
		sectionMap[subkey] = value
	}

	if include == nil {
		return allProfiles, nil
	}

	profiles := make(map[string]any, len(include))
	seen := make(map[string]struct{}, len(include))
	for _, name := range include {
		name = strings.TrimSpace(name)
		if name == "" {
			return nil, fmt.Errorf("publish.profiles.include contains an empty profile name")
		}
		if _, duplicate := seen[name]; duplicate {
			continue
		}
		seen[name] = struct{}{}
		profile, ok := allProfiles[name]
		if !ok {
			return nil, fmt.Errorf("publish profile %q not found in runtime profile source", name)
		}
		profiles[name] = profile
	}
	return profiles, nil
}

func runtimeSectionFromConfig(cfg boot.Config, section string) map[string]any {
	values := make(map[string]any)
	if cfg == nil {
		return values
	}

	prefix := section + "."
	for _, key := range cfg.Keys() {
		if !strings.HasPrefix(key, prefix) {
			continue
		}
		value, _ := cfg.Get(key)
		values[strings.TrimPrefix(key, prefix)] = value
	}
	return values
}

func runtimeMetadataMap(metadata attrs.Bag) (map[string]any, error) {
	if metadata == nil {
		return nil, fmt.Errorf("metadata bag is nil")
	}

	raw, exists := metadata[publishRuntimeMetadataKey]
	if !exists {
		runtime := make(map[string]any)
		metadata[publishRuntimeMetadataKey] = runtime
		return runtime, nil
	}

	switch typed := raw.(type) {
	case map[string]any:
		return typed, nil
	case attrs.Bag:
		runtime := map[string]any(typed)
		metadata[publishRuntimeMetadataKey] = runtime
		return runtime, nil
	default:
		return nil, fmt.Errorf("wippy.yaml metadata.runtime must be a map when publishing runtime profiles")
	}
}

func hasNestedRuntimeMetadata(metadata attrs.Bag, nestedKey string) bool {
	if metadata == nil {
		return false
	}

	dotted := publishRuntimeMetadataKey + "." + nestedKey
	for key := range metadata {
		if key == dotted || strings.HasPrefix(key, dotted+".") {
			return true
		}
	}

	raw, exists := metadata[publishRuntimeMetadataKey]
	if !exists {
		return false
	}
	switch typed := raw.(type) {
	case map[string]any:
		_, exists = typed[nestedKey]
		return exists
	case attrs.Bag:
		_, exists = typed[nestedKey]
		return exists
	default:
		return false
	}
}
