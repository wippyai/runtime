package moduleloader

import (
	"context"
	"testing"

	"github.com/ponyruntime/pony/api/payload"
	regapi "github.com/ponyruntime/pony/api/registry"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap/zaptest"
)

func TestEntryLoader_LoadManifest(t *testing.T) {
	logger := zaptest.NewLogger(t)

	tests := []struct {
		name         string
		entries      []regapi.Entry
		expectedDeps []ManifestDependency
		error        assert.ErrorAssertionFunc
	}{
		{
			name: "single valid dependency",
			entries: []regapi.Entry{
				{
					Kind: regapi.KindDependencyComponent,
					ID:   regapi.ID{Name: "test-component"},
					Data: payload.NewPayload(map[string]any{
						"component": "myapp/test",
						"version":   "1.0.0",
					}, payload.Golang),
				},
			},
			expectedDeps: []ManifestDependency{
				{
					Name:    Name{Organization: "myapp", Module: "test"},
					Version: "1.0.0",
				},
			},
			error: assert.NoError,
		},
		{
			name: "multiple valid dependencies",
			entries: []regapi.Entry{
				{
					Kind: regapi.KindDependencyComponent,
					ID:   regapi.ID{Name: "component1"},
					Data: payload.NewPayload(map[string]any{
						"component": "myorg/app1",
						"version":   "1.0.0",
					}, payload.Golang),
				},
				{
					Kind: regapi.KindDependencyComponent,
					ID:   regapi.ID{Name: "component2"},
					Data: payload.NewPayload(map[string]any{
						"component": "myorg/app2",
						"version":   "2.1.0",
					}, payload.Golang),
				},
			},
			expectedDeps: []ManifestDependency{
				{
					Name:    Name{Organization: "myorg", Module: "app1"},
					Version: "1.0.0",
				},
				{
					Name:    Name{Organization: "myorg", Module: "app2"},
					Version: "2.1.0",
				},
			},
			error: assert.NoError,
		},
		{
			name: "mixed entries with non-dependency components",
			entries: []regapi.Entry{
				{
					Kind: regapi.KindDependencyComponent,
					ID:   regapi.ID{Name: "component1"},
					Data: payload.NewPayload(map[string]any{
						"component": "validorg/validapp",
						"version":   "1.0.0",
					}, payload.Golang),
				},
				{
					Kind: "some-other-kind",
					ID:   regapi.ID{Name: "other"},
					Data: payload.NewPayload(map[string]any{
						"something": "else",
					}, payload.Golang),
				},
			},
			expectedDeps: []ManifestDependency{
				{
					Name:    Name{Organization: "validorg", Module: "validapp"},
					Version: "1.0.0",
				},
			},
			error: assert.NoError,
		},
		{
			name:         "empty entries",
			entries:      []regapi.Entry{},
			expectedDeps: []ManifestDependency{},
			error:        assert.NoError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			loader := NewEntryLoader(tt.entries, logger)

			manifest, err := loader.LoadManifest(context.Background())

			tt.error(t, err)

			if err != nil {
				return
			}

			assert.NotNil(t, manifest)
			assert.Len(t, manifest.Dependencies, len(tt.expectedDeps))

			for i, expected := range tt.expectedDeps {
				actual := manifest.Dependencies[i]
				assert.Equalf(t, expected.Name.Organization, actual.Name.Organization,
					"dependency %d organization mismatch", i)
				assert.Equalf(t, expected.Name.Module, actual.Name.Module,
					"dependency %d module mismatch", i)
				assert.Equalf(t, expected.Version, actual.Version,
					"dependency %d version mismatch", i)
			}
		})
	}
}
