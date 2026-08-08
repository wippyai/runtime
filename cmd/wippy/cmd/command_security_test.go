// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/registry"
)

func TestExtractCommandMeta_Security(t *testing.T) {
	t.Run("no security block", func(t *testing.T) {
		meta := extractCommandMeta(map[string]any{
			"command": map[string]any{"name": "test"},
		})
		require.NotNil(t, meta)
		assert.Nil(t, meta.Security)
	})

	t.Run("actor and policies", func(t *testing.T) {
		meta := extractCommandMeta(map[string]any{
			"command": map[string]any{
				"name": "test",
				"security": map[string]any{
					"actor":    map[string]any{"id": "wippy.test:runner"},
					"policies": []any{"wippy.test:runner_policy"},
					"groups":   []any{"app.security:admin"},
				},
			},
		})
		require.NotNil(t, meta)
		require.NotNil(t, meta.Security)
		assert.Equal(t, "wippy.test:runner", meta.Security.Actor.ID)
		assert.Equal(t, []registry.ID{registry.NewID("wippy.test", "runner_policy")}, meta.Security.Policies)
		assert.Equal(t, []registry.ID{registry.NewID("app.security", "admin")}, meta.Security.PolicyGroups)
	})

	t.Run("empty security block resolves to nil", func(t *testing.T) {
		meta := extractCommandMeta(map[string]any{
			"command": map[string]any{
				"name":     "test",
				"security": map[string]any{"actor": map[string]any{}},
			},
		})
		require.NotNil(t, meta)
		assert.Nil(t, meta.Security)
	})

	t.Run("non-string policy entries are skipped", func(t *testing.T) {
		meta := extractCommandMeta(map[string]any{
			"command": map[string]any{
				"name": "test",
				"security": map[string]any{
					"actor":    map[string]any{"id": "a"},
					"policies": []any{7, ""},
				},
			},
		})
		require.NotNil(t, meta)
		require.NotNil(t, meta.Security)
		assert.Nil(t, meta.Security.Policies)
	})
}
