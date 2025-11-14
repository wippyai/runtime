package bootconfig

import (
	"fmt"
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
		return nil, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse YAML: %w", err)
	}

	if err := validateVersion(cfg.Version); err != nil {
		return nil, err
	}

	return buildBootConfig(cfg.Sections)
}

func validateVersion(version string) error {
	if version == "" {
		return fmt.Errorf("missing 'version' field in config file")
	}

	supported := []string{"1.0"}
	for _, v := range supported {
		if version == v {
			return nil
		}
	}

	return fmt.Errorf("unsupported config version: %s (supported: %v)", version, supported)
}

func buildBootConfig(sections map[string]map[string]any) (boot.Config, error) {
	if len(sections) == 0 {
		return nil, nil
	}

	opts := make([]boot.ConfigOption, 0, len(sections))
	for name, values := range sections {
		if name == "version" {
			continue
		}
		opts = append(opts, boot.WithSection(name, values))
	}

	return boot.NewConfig(opts...), nil
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
