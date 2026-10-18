// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/boot/deps/lock"
)

func TestWorkspaceReplacementDeclaredVersion(t *testing.T) {
	for _, operation := range []string{"resolve", "update-all", "update-module"} {
		t.Run(operation, func(t *testing.T) {
			handler := lockedResolutionHandler(t, []lock.Module{{Name: "acme/module", Version: "1.0.0"}})
			source := t.TempDir()
			require.NoError(t, os.WriteFile(filepath.Join(source, "wippy.yaml"), []byte("version: 2.0.0\n"), 0o600))
			require.NoError(t, os.WriteFile(filepath.Join(source, "_index.yaml"), []byte("namespace: acme.module\nentries:\n  - name: definition\n    kind: ns.definition\n"), 0o600))
			handler.replacements["acme/module"] = lock.Replacement{From: "acme/module", To: source}
			roots := []DependencyDefinition{{Component: "acme/module", Version: "*"}}
			var resolved []ResolvedModule
			var err error
			switch operation {
			case "resolve":
				resolved, err = handler.ResolveWorkspaceDependencies(newTestContext(), roots)
			case "update-all":
				resolved, err = handler.UpdateWorkspaceDependencies(newTestContext(), roots, nil)
			case "update-module":
				resolved, err = handler.UpdateWorkspaceDependencies(newTestContext(), roots, []string{"acme/module"})
			}
			require.NoError(t, err)
			require.Len(t, resolved, 1)
			require.Equal(t, "2.0.0", resolved[0].Version)
			require.Equal(t, "1.0.0", handler.lock.GetModules()[0].Version)
		})
	}
}
