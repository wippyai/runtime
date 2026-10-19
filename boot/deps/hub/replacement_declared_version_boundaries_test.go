// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/boot/deps/lock"
)

func TestWorkspaceReplacementDeclaredVersionConstraints(t *testing.T) {
	for _, offline := range []bool{false, true} {
		for _, constraint := range []string{"*", "2.0.0", "1.0.0", ">=3.0.0"} {
			t.Run(fmt.Sprintf("offline=%t/constraint=%s", offline, constraint), func(t *testing.T) {
				handler := lockedResolutionHandler(t, []lock.Module{{Name: "acme/module", Version: "1.0.0"}, {Name: "acme/unrelated", Version: "5.0.0"}})
				addDeclaredVersionReplacement(t, handler, "2.0.0")
				ctx := newTestContext()
				if offline {
					ctx = regapi.WithDependencyAccess(ctx, regapi.DependencyAccessVerifiedOffline)
				}
				locked := handler.workspaceLockedVersions(nil, false)
				resolved, err := handler.ResolveWorkspaceDependencies(ctx, []DependencyDefinition{{Component: "acme/module", Version: constraint}})
				if constraint == "*" || constraint == "2.0.0" {
					require.NoError(t, err)
					require.Len(t, resolved, 1)
					require.Equal(t, "2.0.0", resolved[0].Version)
				} else {
					require.Error(t, err, "a source version must not bypass the graph's constraints")
				}
				require.Equal(t, locked, handler.workspaceLockedVersions(nil, false), "resolution must not mutate existing lock selections")
			})
		}
	}
}

func TestWorkspaceReplacementDeclaredVersionWithoutLockSelection(t *testing.T) {
	handler := lockedResolutionHandler(t, nil)
	addDeclaredVersionReplacement(t, handler, "v2.0.0")
	resolved, err := handler.ResolveWorkspaceDependencies(newTestContext(), []DependencyDefinition{{Component: "acme/module", Version: "*"}})
	require.NoError(t, err)
	require.Len(t, resolved, 1)
	require.Equal(t, "v2.0.0", resolved[0].Version)
	require.Empty(t, handler.lock.GetModules())
}

func TestWorkspaceUnversionedReplacementKeepsSelection(t *testing.T) {
	handler := lockedResolutionHandler(t, []lock.Module{{Name: "acme/module", Version: "1.0.0"}})
	addDeclaredVersionReplacement(t, handler, "")
	resolved, err := handler.ResolveWorkspaceDependencies(newTestContext(), []DependencyDefinition{{Component: "acme/module", Version: "*"}})
	require.NoError(t, err)
	require.Len(t, resolved, 1)
	require.Equal(t, "1.0.0", resolved[0].Version)
}

func TestTransitiveReplacementDeclaredVersion(t *testing.T) {
	handler := lockedResolutionHandler(t, []lock.Module{{Name: "acme/parent", Version: "1.0.0"}, {Name: "acme/module", Version: "1.0.0"}})
	addDeclaredVersionReplacement(t, handler, "2.0.0")
	handler.hub = &fakeHub{getManifest: func(_ context.Context, org, name, version string) (*ModuleManifest, error) {
		require.Equal(t, "parent", name, "the replaced child must never ask the Hub for its source")
		return &ModuleManifest{Org: org, Name: name, Version: version,
			Dependencies: []ManifestDep{{Org: "acme", Name: "module", Constraint: ">=2.0.0"}}}, nil
	}}
	resolved, err := handler.ResolveWorkspaceDependencies(newTestContext(), []DependencyDefinition{{Component: "acme/parent", Version: "1.0.0"}})
	require.NoError(t, err)
	selected, ok := selectedModuleVersion(resolved, "acme/module")
	require.True(t, ok)
	require.Equal(t, "2.0.0", selected)
}

func addDeclaredVersionReplacement(t *testing.T, handler *DependencyHandler, version string) {
	t.Helper()
	source := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(source, "wippy.yaml"), []byte("version: "+version+"\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(source, "_index.yaml"), []byte("namespace: acme.module\nentries:\n  - name: definition\n    kind: ns.definition\n"), 0o600))
	handler.replacements["acme/module"] = lock.Replacement{From: "acme/module", To: source}
}
