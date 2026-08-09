// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"fmt"
	"strings"

	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/boot/deps/config"
)

// addPublishedRuntimeProfileMetadata retains the focused helper used by
// callers and tests which only configure profile publication.
func addPublishedRuntimeProfileMetadata(metadata attrs.Bag, configDir string, profileCfg config.PublishProfilesConfig) error {
	return addPublishedRuntimeMetadata(metadata, configDir, config.PublishConfig{Profiles: profileCfg})
}

func collectPublishedRuntimeProfiles(dst *publishedRuntimeConfig, configDir string, profileCfg config.PublishProfilesConfig) error {
	if profileCfg.Enabled != nil && !*profileCfg.Enabled {
		return nil
	}

	cfg, source, err := loadPublishRuntimeSource(configDir, profileCfg.Source)
	if err != nil {
		return err
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
	if path, found := findPublishEnvReference(profiles, publishRuntimeProfilesMetadataKey); found {
		return fmt.Errorf("runtime setting %s references the publisher environment; use a runtime variable or an entry *_env setting instead", path)
	}
	dst.profiles = profiles
	mergePublishedVars(dst.vars, runtimeSectionFromConfig(cfg, publishRuntimeVarsMetadataKey), source)
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
