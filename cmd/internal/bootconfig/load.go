package bootconfig

import (
	"os"

	"github.com/wippyai/runtime/api/boot"
	"gopkg.in/yaml.v3"
)

type config struct {
	Version  string                    `yaml:"version"`
	Sections map[string]map[string]any `yaml:",inline"`
}

func Load(path string) (boot.Config, error) {
	if path == "" {
		return nil, nil //nolint:nilnil // empty path means no config
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
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

		// Handle map[interface{}]interface{} from yaml.v3
		if nestedInterface, ok := v.(map[interface{}]interface{}); ok {
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
