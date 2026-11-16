// SPDX-License-Identifier: MPL-2.0

package stages

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
)

// A reusable artifact must preserve literal contributions and requirement
// declarations without materializing a default that deployment may replace.
func TestLink_DeferredDefaultAppendComposition(t *testing.T) {
	for _, path := range []string{".groups +=", ".meta.groups +="} {
		for _, selected := range []string{"", "app:restricted"} {
			t.Run(path+"/"+selected, func(t *testing.T) {
				ctx, _ := setupTestContext()
				target := registry.Entry{ID: registry.NewID("test", "target"), Kind: "security.policy", Data: payload.New(map[string]any{})}
				if path == ".groups +=" {
					target.Data = payload.New(map[string]any{"groups": []any{"app:literal"}})
				} else {
					target.Meta = attrs.Bag{"groups": []any{"app:literal"}}
				}
				declaration := map[string]any{"default": "app:default", "targets": []any{map[string]any{"entry": "target", "path": path}}}
				entries := []registry.Entry{
					{ID: registry.NewID("test", "group"), Kind: registry.NamespaceRequirement, Registry: registry.EntryMetadata{Owner: "test/module"}, Data: payload.New(declaration)}, target,
				}
				require.NoError(t, executeLinkFixture(ctx, &entries, Link(WithDeferredDefaultAppends())))
				groups := func() any {
					e := findEntry(entries, "test", "target")
					if path == ".groups +=" {
						return e.Data.Data().(map[string]any)["groups"]
					}
					return e.Meta["groups"]
				}
				require.Equal(t, []any{"app:literal"}, groups())
				require.Equal(t, declaration, findEntry(entries, "test", "group").Data.Data())
				if selected != "" {
					entries = append(entries, registry.Entry{ID: registry.NewID("app", "dependency"), Kind: registry.NamespaceDependency, Data: payload.New(map[string]any{"component": "test/module", "parameters": []any{map[string]any{"name": "group", "value": selected}}})})
				}
				require.NoError(t, executeLinkFixture(ctx, &entries, Link()))
				want := "app:default"
				if selected != "" {
					want = selected
				}
				require.Equal(t, []any{"app:literal", want}, groups())
			})
		}
	}
}

func TestLink_DeferredDefaultAppendStillValidatesTargets(t *testing.T) {
	for _, tc := range []struct {
		data               map[string]any
		meta               attrs.Bag
		name, path, target string
	}{
		{name: "missing", path: ".groups +=", target: "missing", data: map[string]any{}},
		{name: "scalar", path: ".groups +=", target: "target", data: map[string]any{"groups": 42}},
		{name: "metadata scalar", path: ".meta.groups +=", target: "target", data: map[string]any{}, meta: attrs.Bag{"groups": 42}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, _ := setupTestContext()
			entries := []registry.Entry{
				{ID: registry.NewID("test", "group"), Kind: registry.NamespaceRequirement, Registry: registry.EntryMetadata{Owner: "test/module"}, Data: payload.New(map[string]any{"default": "app:default", "targets": []any{map[string]any{"entry": tc.target, "path": tc.path}}})},
				{ID: registry.NewID("test", "target"), Kind: "security.policy", Data: payload.New(tc.data), Meta: tc.meta},
			}
			require.Error(t, executeLinkFixture(ctx, &entries, Link(WithDeferredDefaultAppends(), WithStrictRequirements())))
			require.Equal(t, tc.data, findEntry(entries, "test", "target").Data.Data())
			require.Equal(t, tc.meta, findEntry(entries, "test", "target").Meta)
		})
	}
}

func TestLink_DeferredDefaultsKeepDirectSetsAndExplicitAppends(t *testing.T) {
	ctx, _ := setupTestContext()
	entries := []registry.Entry{
		{ID: registry.NewID("test", "group"), Kind: registry.NamespaceRequirement, Registry: registry.EntryMetadata{Owner: "test/module"}, Data: payload.New(map[string]any{"default": "app:default", "targets": []any{map[string]any{"entry": "target", "path": ".groups +="}}})},
		{ID: registry.NewID("test", "setting"), Kind: registry.NamespaceRequirement, Registry: registry.EntryMetadata{Owner: "test/module"}, Data: payload.New(map[string]any{"default": false, "targets": []any{map[string]any{"entry": "target", "path": ".enabled"}}})},
		{ID: registry.NewID("test", "target"), Kind: "security.policy", Data: payload.New(map[string]any{})},
		{ID: registry.NewID("app", "dependency"), Kind: registry.NamespaceDependency, Data: payload.New(map[string]any{"component": "test/module", "parameters": []any{map[string]any{"name": "group", "value": "app:explicit"}}})},
	}
	require.NoError(t, executeLinkFixture(ctx, &entries, Link(WithDeferredDefaultAppends())))
	data := findEntry(entries, "test", "target").Data.Data().(map[string]any)
	require.Equal(t, []any{"app:explicit"}, data["groups"])
	require.Equal(t, false, data["enabled"])
}
