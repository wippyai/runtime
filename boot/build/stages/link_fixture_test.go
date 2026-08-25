// SPDX-License-Identifier: MPL-2.0

package stages

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/registry"
)

// Fixtures declare who owns an entry with a "module" meta key and with
// Entry.DependencyRoot. linkFixtureProvenance moves those declarations into the
// provenance the stage reads and removes them from the entries, so a fixture
// can never make the stage read ownership from author payload.
const fixtureModuleKey = "module"

func fixtureEntryModule(entry registry.Entry) string {
	if entry.Meta == nil {
		return ""
	}
	module, _ := entry.Meta[fixtureModuleKey].(string)
	return module
}

func linkFixtureProvenance(prov registry.ProvenanceMap, entries *[]registry.Entry) {
	for i := range *entries {
		entry := &(*entries)[i]
		record := registry.EntryProvenance{Module: fixtureEntryModule(*entry), Root: entry.DependencyRoot}
		entry.DependencyRoot = false
		delete(entry.Meta, fixtureModuleKey)
		prov[entry.ID] = record
	}
}

// linkFixtureStage builds a link stage over the provenance a fixture declares.
func linkFixtureStage(entries *[]registry.Entry, opts ...LinkOption) boot.Stage {
	prov := registry.ProvenanceMap{}
	linkFixtureProvenance(prov, entries)
	return Link(prov, opts...)
}

// A supplied provenance map is total over the entries the stage attributes.
// The nil map is the documented single-source build with no module world.
func TestLinkProvenanceTotality(t *testing.T) {
	target := registry.Entry{
		ID:   registry.NewID("app", "endpoint"),
		Kind: "http.endpoint",
		Meta: map[string]any{"router": "app:default"},
	}
	definition := registry.Entry{
		ID:   registry.NewID("identity.account", "definition"),
		Kind: registry.NamespaceDefinition,
	}

	for _, tc := range []struct {
		prov    registry.ProvenanceMap
		name    string
		wantErr bool
	}{
		{name: "no module world", prov: nil},
		{
			name: "every entry named",
			prov: registry.ProvenanceMap{target.ID: {}, definition.ID: {Module: fixtureComponent}},
		},
		{
			name:    "an entry unnamed",
			prov:    registry.ProvenanceMap{target.ID: {}},
			wantErr: true,
		},
		{
			name:    "empty map names nothing",
			prov:    registry.ProvenanceMap{},
			wantErr: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, _ := setupTestContext()
			entries := []registry.Entry{target, definition}
			err := Link(tc.prov).Execute(ctx, &entries)
			if !tc.wantErr {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, registry.ErrMissingProvenance)
		})
	}
}

func TestLinkProvenanceTotalityCoversExplicitDependencies(t *testing.T) {
	ctx, _ := setupTestContext()
	target := registry.Entry{ID: registry.NewID("app", "endpoint"), Kind: "http.endpoint"}
	dependency := dependencyEntry(fixtureComponent, "router", "app:configured")

	entries := []registry.Entry{target}
	err := Link(registry.ProvenanceMap{target.ID: {}}, WithDependencies([]registry.Entry{dependency})).Execute(ctx, &entries)
	require.ErrorIs(t, err, registry.ErrMissingProvenance,
		"a declaration linked without being resident is attributed too")
}
