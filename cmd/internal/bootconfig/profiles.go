// SPDX-License-Identifier: MPL-2.0

package bootconfig

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/wippyai/runtime/api/boot"
)

const (
	sectionProfiles = "profiles"
	sectionVars     = "vars"
)

var varRefPattern = regexp.MustCompile(`\$\{([^}]+)\}`)

// ApplyProfiles overlays the named profiles, in order, over cfg.
//
// Profiles are stored under the "profiles" section after normal config
// flattening. A profile key has the form:
//
//	profiles.<profile>.<section>.<key>
//
// The resolved config omits the profiles section. The disable section supports
// list operations through namespaces.add/remove and entries.add/remove so small
// profiles can compose without replacing the whole list.
func ApplyProfiles(cfg boot.Config, profileNames []string) (boot.Config, error) {
	if cfg == nil {
		cfg = boot.NewConfig()
	}

	sections := sectionsFromConfig(cfg)
	profiles, err := profileDefinitions(sections[sectionProfiles])
	if err != nil {
		return nil, err
	}
	delete(sections, sectionProfiles)

	for _, profileName := range profileNames {
		profileName = strings.TrimSpace(profileName)
		if profileName == "" {
			continue
		}

		profile, ok := profiles[profileName]
		if !ok {
			return nil, fmt.Errorf("profile %q not found", profileName)
		}
		if err := applyProfile(sections, profile); err != nil {
			return nil, fmt.Errorf("apply profile %q: %w", profileName, err)
		}
	}

	return configFromSections(sections), nil
}

// ResolveVariables expands ${name} references from the vars section across all
// config values. Profile variables do not read OS environment values; the
// .wippy.yaml loader resolves scoped ${env:NAME} references before profiles run.
func ResolveVariables(cfg boot.Config) (boot.Config, error) {
	if cfg == nil {
		return nil, nil //nolint:nilnil // preserve existing nil-config behavior
	}

	sections := sectionsFromConfig(cfg)
	vars := sections[sectionVars]
	for section, values := range sections {
		for key, value := range values {
			resolved, err := resolveValue(value, vars)
			if err != nil {
				return nil, fmt.Errorf("resolve %s.%s: %w", section, key, err)
			}
			values[key] = resolved
		}
	}

	return configFromSections(sections), nil
}

func sectionsFromConfig(cfg boot.Config) map[string]map[string]any {
	sections := make(map[string]map[string]any)
	if cfg == nil {
		return sections
	}

	for _, key := range cfg.Keys() {
		section, subkey, ok := strings.Cut(key, boot.ConfigSep)
		if !ok || section == "" || subkey == "" {
			continue
		}
		value, exists := cfg.Get(key)
		if !exists {
			continue
		}
		if sections[section] == nil {
			sections[section] = make(map[string]any)
		}
		sections[section][subkey] = value
	}

	return sections
}

func configFromSections(sections map[string]map[string]any) boot.Config {
	opts := make([]boot.ConfigOption, 0, len(sections))
	for section, values := range sections {
		opts = append(opts, boot.WithSection(section, values))
	}
	return boot.NewConfig(opts...)
}

type profileDefinition map[string]map[string]any

func profileDefinitions(raw map[string]any) (map[string]profileDefinition, error) {
	profiles := make(map[string]profileDefinition)
	for key, value := range raw {
		profileName, rest, ok := strings.Cut(key, boot.ConfigSep)
		if !ok || profileName == "" || rest == "" {
			return nil, fmt.Errorf("invalid profile key %q", key)
		}
		section, subkey, ok := strings.Cut(rest, boot.ConfigSep)
		if !ok || section == "" || subkey == "" {
			return nil, fmt.Errorf("invalid profile key %q", key)
		}

		if profiles[profileName] == nil {
			profiles[profileName] = make(profileDefinition)
		}
		if profiles[profileName][section] == nil {
			profiles[profileName][section] = make(map[string]any)
		}
		profiles[profileName][section][subkey] = value
	}
	return profiles, nil
}

func applyProfile(sections map[string]map[string]any, profile profileDefinition) error {
	for section, overlay := range profile {
		if sections[section] == nil {
			sections[section] = make(map[string]any)
		}

		if section == "disable" {
			if err := applyDisableOverlay(sections[section], overlay); err != nil {
				return err
			}
			continue
		}

		for key, value := range overlay {
			sections[section][key] = value
		}
	}
	return nil
}

func applyDisableOverlay(dst, overlay map[string]any) error {
	for key, value := range overlay {
		if isListOp(key, "namespaces") || isListOp(key, "entries") {
			continue
		}
		dst[key] = value
	}

	if err := applyStringListOps(dst, overlay, "namespaces"); err != nil {
		return err
	}
	if err := applyStringListOps(dst, overlay, "entries"); err != nil {
		return err
	}
	return nil
}

func isListOp(key, base string) bool {
	return key == base+".add" || key == base+".remove"
}

func applyStringListOps(dst, overlay map[string]any, key string) error {
	add, hasAdd := overlay[key+".add"]
	remove, hasRemove := overlay[key+".remove"]
	if !hasAdd && !hasRemove {
		return nil
	}

	values, err := stringSliceValue(dst[key])
	if err != nil {
		return fmt.Errorf("invalid disable.%s: %w", key, err)
	}

	if hasAdd {
		addValues, err := stringSliceValue(add)
		if err != nil {
			return fmt.Errorf("invalid disable.%s.add: %w", key, err)
		}
		for _, item := range addValues {
			if !slices.Contains(values, item) {
				values = append(values, item)
			}
		}
	}

	if hasRemove {
		removeValues, err := stringSliceValue(remove)
		if err != nil {
			return fmt.Errorf("invalid disable.%s.remove: %w", key, err)
		}
		values = slices.DeleteFunc(values, func(item string) bool {
			return slices.Contains(removeValues, item)
		})
	}

	dst[key] = values
	return nil
}

func stringSliceValue(value any) ([]string, error) {
	if value == nil {
		return nil, nil
	}

	switch typed := value.(type) {
	case []string:
		return append([]string(nil), typed...), nil
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			s, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("expected string item, got %T", item)
			}
			out = append(out, s)
		}
		return out, nil
	default:
		return nil, fmt.Errorf("expected string list, got %T", value)
	}
}

func resolveValue(value any, vars map[string]any) (any, error) {
	switch typed := value.(type) {
	case string:
		return resolveString(typed, vars)
	case []string:
		out := make([]string, len(typed))
		for i, item := range typed {
			resolved, err := resolveString(item, vars)
			if err != nil {
				return nil, err
			}
			s, ok := resolved.(string)
			if !ok {
				return nil, fmt.Errorf("list item resolved to %T, want string", resolved)
			}
			out[i] = s
		}
		return out, nil
	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			resolved, err := resolveValue(item, vars)
			if err != nil {
				return nil, err
			}
			out[i] = resolved
		}
		return out, nil
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			resolved, err := resolveValue(item, vars)
			if err != nil {
				return nil, err
			}
			out[key] = resolved
		}
		return out, nil
	default:
		return value, nil
	}
}

func resolveString(value string, vars map[string]any) (any, error) {
	matches := varRefPattern.FindAllStringSubmatchIndex(value, -1)
	if len(matches) == 0 {
		return value, nil
	}

	if len(matches) == 1 && matches[0][0] == 0 && matches[0][1] == len(value) {
		name := value[matches[0][2]:matches[0][3]]
		return lookupVariable(name, vars)
	}

	var b strings.Builder
	last := 0
	for _, match := range matches {
		b.WriteString(value[last:match[0]])
		name := value[match[2]:match[3]]
		replacement, err := lookupVariable(name, vars)
		if err != nil {
			return nil, err
		}
		b.WriteString(fmt.Sprint(replacement))
		last = match[1]
	}
	b.WriteString(value[last:])

	return b.String(), nil
}

func lookupVariable(name string, vars map[string]any) (any, error) {
	name = strings.TrimSpace(name)
	if strings.HasPrefix(name, "env.") {
		return nil, fmt.Errorf("OS environment interpolation %q is not supported; declare an env.variable entry and reference it by ID", name)
	}
	name = strings.TrimPrefix(name, "vars.")
	if name == "" {
		return nil, fmt.Errorf("empty variable reference")
	}
	if value, ok := vars[name]; ok {
		return value, nil
	}
	return nil, fmt.Errorf("variable %q not found", name)
}
