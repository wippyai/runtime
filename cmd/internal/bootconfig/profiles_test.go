// SPDX-License-Identifier: MPL-2.0

package bootconfig

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/boot"
)

func TestApplyProfiles_MultipleProfilesComposeInOrder(t *testing.T) {
	cfg := boot.NewConfig(
		boot.WithSection("vars", map[string]any{
			"port": 8085,
		}),
		boot.WithSection("override", map[string]any{
			"app:gateway:addr": ":${port}",
			"app:db:kind":      "db.sql.sqlite",
		}),
		boot.WithSection("disable", map[string]any{
			"namespaces": []string{"legacy.**"},
		}),
		boot.WithSection("profiles", map[string]any{
			"pg.vars.port":                   18085,
			"pg.override.app:db:kind":        "db.sql.postgres",
			"pg.disable.namespaces.add":      []any{"kickside.research.**", "legacy.**"},
			"lean.disable.namespaces.remove": []any{"legacy.**"},
			"lean.disable.entries.add":       []any{"app:heavy_worker"},
		}),
	)

	resolved, err := ApplyProfiles(cfg, []string{"pg", "lean"})
	require.NoError(t, err)

	require.Equal(t, 18085, resolved.GetInt("vars.port", 0))
	require.Equal(t, "db.sql.postgres", resolved.GetString("override.app:db:kind", ""))
	require.Equal(t, []string{"kickside.research.**"}, valueAsStringSlice(t, resolved, "disable.namespaces"))
	require.Equal(t, []string{"app:heavy_worker"}, valueAsStringSlice(t, resolved, "disable.entries"))
	require.Empty(t, resolved.Sub("profiles").Keys())
}

func TestApplyProfiles_MissingProfileErrors(t *testing.T) {
	cfg := boot.NewConfig(boot.WithSection("profiles", map[string]any{
		"dev.vars.port": 8085,
	}))

	_, err := ApplyProfiles(cfg, []string{"prod"})
	require.Error(t, err)
	require.Contains(t, err.Error(), `profile "prod" not found`)
}

func TestApplyProfiles_InvalidDisableListErrors(t *testing.T) {
	cfg := boot.NewConfig(boot.WithSection("profiles", map[string]any{
		"bad.disable.namespaces.add": []any{"ok", 42},
	}))

	_, err := ApplyProfiles(cfg, []string{"bad"})
	require.Error(t, err)
	require.Contains(t, err.Error(), "disable.namespaces.add")
}

func TestResolveVariables_ExpandsVarsAfterProfiles(t *testing.T) {
	cfg := boot.NewConfig(
		boot.WithSection("vars", map[string]any{
			"port":       18085,
			"db_kind":    "db.sql.postgres",
			"worker_cnt": 8,
		}),
		boot.WithSection("override", map[string]any{
			"app:gateway:addr":      ":${port}",
			"app:db:kind":           "${db_kind}",
			"app:processes:workers": "${worker_cnt}",
		}),
	)

	resolved, err := ResolveVariables(cfg)
	require.NoError(t, err)
	require.Equal(t, ":18085", resolved.GetString("override.app:gateway:addr", ""))
	require.Equal(t, "db.sql.postgres", resolved.GetString("override.app:db:kind", ""))

	workers, ok := resolved.Get("override.app:processes:workers")
	require.True(t, ok)
	require.Equal(t, 8, workers)
}

func TestResolveVariables_RejectsOSEnvInterpolation(t *testing.T) {
	cfg := boot.NewConfig(
		boot.WithSection("vars", map[string]any{}),
		boot.WithSection("override", map[string]any{
			"app:db:password": "${env.KICKSIDE_PG_PASSWORD}",
		}),
	)

	_, err := ResolveVariables(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "OS environment interpolation")
}

func TestResolveVariables_MissingVarErrors(t *testing.T) {
	cfg := boot.NewConfig(boot.WithSection("override", map[string]any{
		"app:gateway:addr": ":${port}",
	}))

	_, err := ResolveVariables(cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), `variable "port" not found`)
}

func valueAsStringSlice(t *testing.T, cfg boot.Config, key string) []string {
	t.Helper()

	value, ok := cfg.Get(key)
	require.True(t, ok)
	out, ok := value.([]string)
	require.True(t, ok, "value %s is %T", key, value)
	return out
}
