// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"fmt"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
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

var publishEnvReference = regexp.MustCompile(`\$\{env:[A-Za-z_][A-Za-z0-9_]*\}`)
var publishVarReference = regexp.MustCompile(`\$\{([^}]+)\}`)

var nonPublishableRuntimeSections = map[string]struct{}{
	"boot":       {}, // derived by the runtime for the destination workspace
	"extensions": {}, // native extension paths belong to the build machine
	"workspace":  {}, // replacements and source roots belong to the workspace
}

type publishedRuntimeConfig struct {
	sections map[string]map[string]any
	profiles map[string]any
	vars     map[string]publishedRuntimeVar
}

type publishedRuntimeVar struct {
	value          any
	source         string
	conflictSource string
}

func addPublishedRuntimeMetadata(metadata attrs.Bag, configDir string, publishCfg config.PublishConfig) error {
	if hasNestedRuntimeMetadata(metadata, publishRuntimeProfilesMetadataKey) {
		return fmt.Errorf("wippy.yaml metadata.runtime.profiles is not supported; declare publishable profiles in .wippy.yaml or publish.profiles.source")
	}

	collected := publishedRuntimeConfig{
		sections: make(map[string]map[string]any),
		vars:     make(map[string]publishedRuntimeVar),
	}

	if err := collectPublishedRuntimeSections(&collected, configDir, publishCfg.Runtime); err != nil {
		return err
	}
	if err := collectPublishedRuntimeProfiles(&collected, configDir, publishCfg.Profiles); err != nil {
		return err
	}
	if len(collected.sections) == 0 && len(collected.profiles) == 0 {
		return nil
	}
	vars, err := publishedRuntimeVars(collected)
	if err != nil {
		return err
	}

	for section := range collected.sections {
		if hasNestedRuntimeMetadata(metadata, section) {
			return fmt.Errorf("runtime section %q is defined in both publish.runtime.sections and wippy.yaml metadata", section)
		}
	}
	if len(vars) > 0 && hasNestedRuntimeMetadata(metadata, publishRuntimeVarsMetadataKey) {
		return fmt.Errorf("runtime vars are defined in both the runtime source and wippy.yaml metadata")
	}

	runtime, err := runtimeMetadataMap(metadata)
	if err != nil {
		return err
	}
	for section, values := range collected.sections {
		runtime[section] = values
	}
	if len(collected.profiles) > 0 {
		runtime[publishRuntimeProfilesMetadataKey] = collected.profiles
	}
	if len(vars) > 0 {
		runtime[publishRuntimeVarsMetadataKey] = vars
	}
	return nil
}

func publishedRuntimeVars(collected publishedRuntimeConfig) (map[string]any, error) {
	// Profile publication predates section publication and intentionally exposes
	// base vars so destination-local config can reference those public defaults.
	// Keep that contract, but read source literally and reject environment refs.
	if len(collected.profiles) > 0 {
		return allPublishedVars(collected.vars)
	}
	return referencedPublishedVars(collected.vars, collected.sections, nil)
}

func allPublishedVars(available map[string]publishedRuntimeVar) (map[string]any, error) {
	selected := make(map[string]any, len(available))
	for name, variable := range available {
		if variable.conflictSource != "" {
			return nil, fmt.Errorf("runtime variable %q has conflicting values in %s and %s", name, variable.source, variable.conflictSource)
		}
		if path, found := findPublishEnvReference(variable.value, publishRuntimeVarsMetadataKey+"."+name); found {
			return nil, fmt.Errorf("runtime setting %s references the publisher environment; use a public default or omit it", path)
		}
		selected[name] = variable.value
	}
	return selected, nil
}

func collectPublishedRuntimeSections(dst *publishedRuntimeConfig, configDir string, runtimeCfg config.PublishRuntimeConfig) error {
	if runtimeCfg.Sections == nil {
		if strings.TrimSpace(runtimeCfg.Source) != "" {
			return fmt.Errorf("publish.runtime.source requires an explicit publish.runtime.sections allow-list")
		}
		return nil
	}

	cfg, source, err := loadPublishRuntimeSource(configDir, runtimeCfg.Source)
	if err != nil {
		return err
	}
	if cfg == nil && len(runtimeCfg.Sections) > 0 {
		return fmt.Errorf("runtime publish source %s does not exist", source)
	}

	seen := make(map[string]struct{}, len(runtimeCfg.Sections))
	for _, rawSection := range runtimeCfg.Sections {
		section := strings.TrimSpace(rawSection)
		if section == "" || strings.Contains(section, ".") {
			return fmt.Errorf("publish.runtime.sections contains invalid top-level section %q", rawSection)
		}
		if _, reserved := nonPublishableRuntimeSections[section]; reserved {
			return fmt.Errorf("runtime section %q is machine-local and cannot be published", section)
		}
		if section == publishRuntimeProfilesMetadataKey || section == publishRuntimeVarsMetadataKey || section == "version" {
			return fmt.Errorf("runtime section %q has dedicated publication semantics and cannot be listed in publish.runtime.sections", section)
		}
		if _, duplicate := seen[section]; duplicate {
			continue
		}
		seen[section] = struct{}{}

		values := runtimeSectionFromConfig(cfg, section)
		if len(values) == 0 {
			return fmt.Errorf("publish runtime section %q not found in %s", section, source)
		}
		if path, found := findPublishEnvReference(values, section); found {
			return fmt.Errorf("runtime setting %s references the publisher environment; use a runtime variable or an entry *_env setting instead", path)
		}
		dst.sections[section] = values
	}

	if len(dst.sections) > 0 {
		mergePublishedVars(dst.vars, runtimeSectionFromConfig(cfg, publishRuntimeVarsMetadataKey), source)
	}
	return nil
}

func loadPublishRuntimeSource(configDir, configured string) (boot.Config, string, error) {
	source := strings.TrimSpace(configured)
	if source == "" {
		source = defaultConfigFile
	}
	if !filepath.IsAbs(source) {
		source = filepath.Join(configDir, source)
	}

	cfg, err := bootconfig.LoadForPublish(source)
	if err != nil {
		return nil, source, fmt.Errorf("load runtime publish source %s: %w", source, err)
	}
	return cfg, source, nil
}

func mergePublishedVars(dst map[string]publishedRuntimeVar, values map[string]any, source string) {
	for key, value := range values {
		existing, exists := dst[key]
		if !exists {
			dst[key] = publishedRuntimeVar{value: value, source: source}
			continue
		}
		if !reflect.DeepEqual(existing.value, value) {
			existing.conflictSource = source
			dst[key] = existing
		}
	}
}

func referencedPublishedVars(available map[string]publishedRuntimeVar, sections map[string]map[string]any, profiles map[string]any) (map[string]any, error) {
	selected := make(map[string]any)
	if err := selectReferencedVars(available, nil, sections, "published runtime sections", selected); err != nil {
		return nil, err
	}

	profileNames := make([]string, 0, len(profiles))
	for name := range profiles {
		profileNames = append(profileNames, name)
	}
	sort.Strings(profileNames)
	for _, name := range profileNames {
		profile, ok := profiles[name].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("published profile %q has invalid type %T", name, profiles[name])
		}
		profileVars, _ := profile[publishRuntimeVarsMetadataKey].(map[string]any)
		if err := selectReferencedVars(available, profileVars, profile, "profile "+name, selected); err != nil {
			return nil, err
		}
	}
	return selected, nil
}

func selectReferencedVars(available map[string]publishedRuntimeVar, local map[string]any, values any, context string, selected map[string]any) error {
	pending := make(map[string]struct{})
	collectPublishVarReferences(values, pending)
	resolved := make(map[string]struct{})
	for len(pending) > 0 {
		name := firstSortedKey(pending)
		delete(pending, name)
		if _, exists := resolved[name]; exists {
			continue
		}
		resolved[name] = struct{}{}

		if value, exists := local[name]; exists {
			if path, found := findPublishEnvReference(value, context+".vars."+name); found {
				return fmt.Errorf("runtime setting %s references the publisher environment; use a public default or omit it", path)
			}
			collectPublishVarReferences(value, pending)
			continue
		}
		variable, exists := available[name]
		if !exists {
			return fmt.Errorf("%s references undefined runtime variable %q", context, name)
		}
		if variable.conflictSource != "" {
			return fmt.Errorf("runtime variable %q has conflicting values in %s and %s", name, variable.source, variable.conflictSource)
		}
		if path, found := findPublishEnvReference(variable.value, publishRuntimeVarsMetadataKey+"."+name); found {
			return fmt.Errorf("runtime setting %s references the publisher environment; use a public default or omit it", path)
		}
		selected[name] = variable.value
		collectPublishVarReferences(variable.value, pending)
	}
	return nil
}

func firstSortedKey(values map[string]struct{}) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys[0]
}

func collectPublishVarReferences(value any, dst map[string]struct{}) {
	switch typed := value.(type) {
	case string:
		for _, match := range publishVarReference.FindAllStringSubmatch(typed, -1) {
			if len(match) == 2 && !strings.HasPrefix(match[1], "env:") {
				dst[match[1]] = struct{}{}
			}
		}
	case map[string]any:
		for _, nested := range typed {
			collectPublishVarReferences(nested, dst)
		}
	case map[string]map[string]any:
		for _, nested := range typed {
			collectPublishVarReferences(nested, dst)
		}
	case map[any]any:
		for _, nested := range typed {
			collectPublishVarReferences(nested, dst)
		}
	case []any:
		for _, nested := range typed {
			collectPublishVarReferences(nested, dst)
		}
	}
}

func findPublishEnvReference(value any, path string) (string, bool) {
	switch typed := value.(type) {
	case string:
		return path, publishEnvReference.MatchString(typed)
	case map[string]any:
		for key, nested := range typed {
			if foundPath, found := findPublishEnvReference(nested, path+"."+key); found {
				return foundPath, true
			}
		}
	case map[any]any:
		for key, nested := range typed {
			if foundPath, found := findPublishEnvReference(nested, fmt.Sprintf("%s.%v", path, key)); found {
				return foundPath, true
			}
		}
	case []any:
		for idx, nested := range typed {
			if foundPath, found := findPublishEnvReference(nested, fmt.Sprintf("%s[%d]", path, idx)); found {
				return foundPath, true
			}
		}
	}
	return "", false
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
		return nil, fmt.Errorf("wippy.yaml metadata.runtime must be a map when publishing runtime configuration")
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
