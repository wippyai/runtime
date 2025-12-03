package bootconfig

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFlattenMap(t *testing.T) {
	t.Run("flattens single level", func(t *testing.T) {
		input := map[string]any{
			"key1": "value1",
			"key2": 42,
		}

		result := flattenMap(input, "")

		assert.Equal(t, "value1", result["key1"])
		assert.Equal(t, 42, result["key2"])
		assert.Len(t, result, 2)
	})

	t.Run("flattens nested maps", func(t *testing.T) {
		input := map[string]any{
			"top": map[string]any{
				"middle": map[string]any{
					"bottom": "value",
				},
			},
		}

		result := flattenMap(input, "")

		assert.Equal(t, "value", result["top.middle.bottom"])
		assert.Len(t, result, 1)
	})

	t.Run("flattens mixed structure", func(t *testing.T) {
		input := map[string]any{
			"simple": "value",
			"nested": map[string]any{
				"enabled": true,
				"count":   10,
			},
			"deep": map[string]any{
				"level1": map[string]any{
					"level2": "deep_value",
				},
			},
		}

		result := flattenMap(input, "")

		assert.Equal(t, "value", result["simple"])
		assert.Equal(t, true, result["nested.enabled"])
		assert.Equal(t, 10, result["nested.count"])
		assert.Equal(t, "deep_value", result["deep.level1.level2"])
		assert.Len(t, result, 4)
	})

	t.Run("handles map[interface{}]interface{} from yaml", func(t *testing.T) {
		input := map[string]any{
			"section": map[interface{}]interface{}{
				"key":   "value",
				"count": 42,
			},
		}

		result := flattenMap(input, "")

		assert.Equal(t, "value", result["section.key"])
		assert.Equal(t, 42, result["section.count"])
		assert.Len(t, result, 2)
	})

	t.Run("applies prefix", func(t *testing.T) {
		input := map[string]any{
			"key": "value",
		}

		result := flattenMap(input, "prefix")

		assert.Equal(t, "value", result["prefix.key"])
		assert.Len(t, result, 1)
	})
}

func TestLoad(t *testing.T) {
	t.Run("loads valid nested config", func(t *testing.T) {
		yamlContent := `version: "1.0"
metrics:
  buffer:
    size: 10000
  interceptor:
    enabled: true
prometheus:
  enabled: true
  address: localhost:9097
`
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "test.yaml")
		err := os.WriteFile(configPath, []byte(yamlContent), 0644)
		require.NoError(t, err)

		cfg, err := Load(configPath)
		require.NoError(t, err)
		require.NotNil(t, cfg)

		metricsCfg := cfg.Sub("metrics")
		require.NotNil(t, metricsCfg)

		assert.Equal(t, 10000, metricsCfg.GetInt("buffer.size", 0))
		assert.Equal(t, true, metricsCfg.GetBool("interceptor.enabled", false))

		promCfg := cfg.Sub("prometheus")
		require.NotNil(t, promCfg)

		assert.Equal(t, true, promCfg.GetBool("enabled", false))
		assert.Equal(t, "localhost:9097", promCfg.GetString("address", ""))
	})

	t.Run("loads config with Sub() access", func(t *testing.T) {
		yamlContent := `version: "1.0"
supervisor:
  host:
    buffer_size: 2048
    worker_count: 32
`
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "test.yaml")
		err := os.WriteFile(configPath, []byte(yamlContent), 0644)
		require.NoError(t, err)

		cfg, err := Load(configPath)
		require.NoError(t, err)
		require.NotNil(t, cfg)

		supervisorCfg := cfg.Sub("supervisor")
		require.NotNil(t, supervisorCfg)

		assert.Equal(t, 2048, supervisorCfg.GetInt("host.buffer_size", 0))
		assert.Equal(t, 32, supervisorCfg.GetInt("host.worker_count", 0))
	})

	t.Run("returns nil for missing file", func(t *testing.T) {
		cfg, err := Load("/nonexistent/path.yaml")
		assert.NoError(t, err)
		assert.Nil(t, cfg)
	})

	t.Run("returns nil for empty path", func(t *testing.T) {
		cfg, err := Load("")
		assert.NoError(t, err)
		assert.Nil(t, cfg)
	})

	t.Run("returns error for missing version", func(t *testing.T) {
		yamlContent := `metrics:
  enabled: true
`
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "test.yaml")
		err := os.WriteFile(configPath, []byte(yamlContent), 0644)
		require.NoError(t, err)

		cfg, err := Load(configPath)
		assert.Error(t, err)
		assert.Nil(t, cfg)
		assert.Contains(t, err.Error(), "missing 'version' field")
	})

	t.Run("returns error for unsupported version", func(t *testing.T) {
		yamlContent := `version: "2.0"
metrics:
  enabled: true
`
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "test.yaml")
		err := os.WriteFile(configPath, []byte(yamlContent), 0644)
		require.NoError(t, err)

		cfg, err := Load(configPath)
		assert.Error(t, err)
		assert.Nil(t, cfg)
		assert.Contains(t, err.Error(), "unsupported config version")
	})

	t.Run("returns error for invalid yaml", func(t *testing.T) {
		yamlContent := `version: "1.0"
metrics:
  enabled: [invalid yaml structure
`
		tmpDir := t.TempDir()
		configPath := filepath.Join(tmpDir, "test.yaml")
		err := os.WriteFile(configPath, []byte(yamlContent), 0644)
		require.NoError(t, err)

		cfg, err := Load(configPath)
		assert.Error(t, err)
		assert.Nil(t, cfg)
		assert.Contains(t, err.Error(), "failed to parse YAML")
	})
}

func TestBuildBootConfig(t *testing.T) {
	t.Run("builds config from flat sections", func(t *testing.T) {
		sections := map[string]map[string]any{
			"metrics": {
				"enabled": true,
				"size":    100,
			},
		}

		cfg, err := buildBootConfig(sections)
		require.NoError(t, err)
		require.NotNil(t, cfg)

		metricsCfg := cfg.Sub("metrics")
		assert.Equal(t, true, metricsCfg.GetBool("enabled", false))
		assert.Equal(t, 100, metricsCfg.GetInt("size", 0))
	})

	t.Run("builds config from nested sections", func(t *testing.T) {
		sections := map[string]map[string]any{
			"metrics": {
				"buffer": map[string]any{
					"size": 10000,
				},
				"interceptor": map[string]any{
					"enabled": true,
				},
			},
		}

		cfg, err := buildBootConfig(sections)
		require.NoError(t, err)
		require.NotNil(t, cfg)

		metricsCfg := cfg.Sub("metrics")
		assert.Equal(t, 10000, metricsCfg.GetInt("buffer.size", 0))
		assert.Equal(t, true, metricsCfg.GetBool("interceptor.enabled", false))
	})

	t.Run("skips version section", func(t *testing.T) {
		sections := map[string]map[string]any{
			"version": {
				"value": "1.0",
			},
			"metrics": {
				"enabled": true,
			},
		}

		cfg, err := buildBootConfig(sections)
		require.NoError(t, err)
		require.NotNil(t, cfg)

		versionCfg := cfg.Sub("version")
		assert.Len(t, versionCfg.Keys(), 0)
	})

	t.Run("returns nil for empty sections", func(t *testing.T) {
		sections := map[string]map[string]any{}

		cfg, err := buildBootConfig(sections)
		require.NoError(t, err)
		assert.Nil(t, cfg)
	})
}
