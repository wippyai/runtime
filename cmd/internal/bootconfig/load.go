// SPDX-License-Identifier: MPL-2.0

package bootconfig

import (
	"fmt"
	"os"
	"regexp"

	"github.com/wippyai/runtime/api/boot"
	"gopkg.in/yaml.v3"
)

type config struct {
	Sections map[string]map[string]any `yaml:",inline"`
	Version  string                    `yaml:"version"`
}

var osEnvRefPattern = regexp.MustCompile(`\$\{env:([A-Za-z_][A-Za-z0-9_]*)\}`)

// LoadFiles loads required configuration files in order. Later files override
// matching leaves from earlier files.
func LoadFiles(paths []string) (boot.Config, error) {
	var merged boot.Config
	for _, path := range paths {
		if path == "" {
			return nil, fmt.Errorf("load config file: path is empty")
		}
		cfg, err := load(path, true, true)
		if err != nil {
			return nil, fmt.Errorf("load config file %s: %w", path, err)
		}
		merged = Merge(merged, cfg)
	}
	return merged, nil
}

func Load(path string) (boot.Config, error) {
	return load(path, false, true)
}

// LoadForPublish reads declared configuration without expanding references to
// the publisher's environment. Package metadata must be derived from source,
// never from machine-local values present while the package is built.
func LoadForPublish(path string) (boot.Config, error) {
	return load(path, false, false)
}

func load(path string, required, resolveEnv bool) (boot.Config, error) {
	if path == "" {
		return nil, nil //nolint:nilnil // empty path means no config
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) && !required {
			return nil, nil //nolint:nilnil // missing file means no config
		}
		return nil, NewReadConfigFileError(err)
	}

	var cfg config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, NewParseYAMLError(err)
	}

	if err := validateVersion(cfg.Version); err != nil {
		return nil, err
	}
	if resolveEnv {
		if err := resolveOSEnvRefs(cfg.Sections); err != nil {
			return nil, err
		}
	}

	return buildBootConfig(cfg.Sections)
}

func validateVersion(version string) error {
	if version == "" {
		return ErrMissingVersionField
	}

	supported := []string{"1.0"}
	for _, v := range supported {
		if version == v {
			return nil
		}
	}

	return NewUnsupportedVersionError(version)
}

func buildBootConfig(sections map[string]map[string]any) (boot.Config, error) {
	if len(sections) == 0 {
		return nil, nil //nolint:nilnil // empty sections means no config
	}

	opts := make([]boot.ConfigOption, 0, len(sections))
	for name, values := range sections {
		if name == "version" {
			continue
		}
		flattened := flattenMap(values, "")
		opts = append(opts, boot.WithSection(name, flattened))
	}

	return boot.NewConfig(opts...), nil
}

// flattenMap recursively flattens nested maps to dot notation
func flattenMap(m map[string]any, prefix string) map[string]any {
	result := make(map[string]any)
	for k, v := range m {
		key := k
		if prefix != "" {
			key = prefix + "." + k
		}

		// Handle map[string]any
		if nested, ok := v.(map[string]any); ok {
			for nk, nv := range flattenMap(nested, key) {
				result[nk] = nv
			}
			continue
		}

		// Handle map[any]any from yaml.v3
		if nestedInterface, ok := v.(map[any]any); ok {
			converted := make(map[string]any)
			for nk, nv := range nestedInterface {
				if strKey, ok := nk.(string); ok {
					converted[strKey] = nv
				}
			}
			for nk, nv := range flattenMap(converted, key) {
				result[nk] = nv
			}
			continue
		}

		result[key] = v
	}
	return result
}

func resolveOSEnvRefs(sections map[string]map[string]any) error {
	for _, values := range sections {
		if err := resolveMapOSEnvRefs(values); err != nil {
			return err
		}
	}
	return nil
}

func resolveMapOSEnvRefs(values map[string]any) error {
	for key, value := range values {
		resolved, err := resolveValueOSEnvRefs(value)
		if err != nil {
			return err
		}
		values[key] = resolved
	}
	return nil
}

func resolveValueOSEnvRefs(value any) (any, error) {
	switch v := value.(type) {
	case string:
		var err error
		resolved := osEnvRefPattern.ReplaceAllStringFunc(v, func(match string) string {
			if err != nil {
				return match
			}
			groups := osEnvRefPattern.FindStringSubmatch(match)
			if len(groups) != 2 {
				return match
			}
			value, exists := os.LookupEnv(groups[1])
			if !exists {
				err = NewMissingOSEnvError(groups[1])
				return match
			}
			return value
		})
		if err != nil {
			return nil, err
		}
		return resolved, nil
	case map[string]any:
		if err := resolveMapOSEnvRefs(v); err != nil {
			return nil, err
		}
		return v, nil
	case map[any]any:
		for key, nested := range v {
			resolved, err := resolveValueOSEnvRefs(nested)
			if err != nil {
				return nil, err
			}
			v[key] = resolved
		}
		return v, nil
	case []any:
		for i, nested := range v {
			resolved, err := resolveValueOSEnvRefs(nested)
			if err != nil {
				return nil, err
			}
			v[i] = resolved
		}
		return v, nil
	default:
		return value, nil
	}
}

func Merge(base, override boot.Config) boot.Config {
	if base == nil {
		return override
	}
	if override == nil {
		return base
	}

	baseKeys := base.Keys()
	overrideKeys := override.Keys()

	merged := make(map[string]map[string]any)

	for _, key := range baseKeys {
		val, _ := base.Get(key)
		parts := splitKey(key)
		if len(parts) != 2 {
			continue
		}
		section, subkey := parts[0], parts[1]
		if merged[section] == nil {
			merged[section] = make(map[string]any)
		}
		merged[section][subkey] = val
	}

	for _, key := range overrideKeys {
		val, _ := override.Get(key)
		parts := splitKey(key)
		if len(parts) != 2 {
			continue
		}
		section, subkey := parts[0], parts[1]
		if merged[section] == nil {
			merged[section] = make(map[string]any)
		}
		merged[section][subkey] = val
	}

	opts := make([]boot.ConfigOption, 0, len(merged))
	for section, values := range merged {
		opts = append(opts, boot.WithSection(section, values))
	}

	return boot.NewConfig(opts...)
}

func splitKey(key string) []string {
	lastDot := -1
	for i, c := range key {
		if c == '.' {
			lastDot = i
			break
		}
	}
	if lastDot == -1 {
		return []string{key}
	}
	return []string{key[:lastDot], key[lastDot+1:]}
}
