// SPDX-License-Identifier: MPL-2.0

package cmd

import (
	"context"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/boot/deps/config"
	appinit "github.com/wippyai/runtime/cmd/internal/app"
	"github.com/wippyai/wapp"
)

func TestReusablePackDefersAppendDefaultsAndEmbedsDirectSetDefaults(t *testing.T) {
	for _, route := range []string{"selected module", "publish"} {
		t.Run(route, func(t *testing.T) {
			root := t.TempDir()
			require.NoError(t, os.MkdirAll(filepath.Join(root, "src"), 0755))
			require.NoError(t, os.MkdirAll(filepath.Join(root, "static"), 0755))
			require.NoError(t, os.WriteFile(filepath.Join(root, "static", "index.html"), []byte("module asset"), 0644))
			require.NoError(t, os.WriteFile(filepath.Join(root, "src", "_index.yaml"), []byte(`version: "1.0"
namespace: acme.ui
entries:
 - name: definition
   kind: ns.definition
 - name: group
   kind: ns.requirement
   default: app:members
   targets: [{entry: policy, path: ".groups +="}]
 - name: asset_path
   kind: ns.requirement
   default: ./static
   targets: [{entry: assets, path: .directory}]
 - name: policy
   kind: security.policy
   groups: [app:literal]
   policy: {actions: access, resources: app:module, effect: allow}
 - name: assets
   kind: fs.directory
`), 0644))
			output := filepath.Join(root, "module.wapp")
			if route == "publish" {
				app, err := appinit.Init(context.Background(), false, false, false, true, time.Now())
				require.NoError(t, err)
				_, err = packModule(app.Ctx, app, &config.ModuleConfig{Organization: "acme", ModuleName: "ui", Version: "1.0.0"}, root, output, []string{"assets"})
				require.NoError(t, err)
			} else {
				require.NoError(t, os.WriteFile(filepath.Join(root, "wippy.yaml"), []byte("organization: acme\nmodule: ui\nversion: 1.0.0\nembed: [acme.ui:assets]\n"), 0644))
				require.NoError(t, os.WriteFile(filepath.Join(root, "wippy.lock"), []byte("directories: {modules: .wippy, src: ./src}\nmodules: []\n"), 0644))
				require.NoError(t, os.WriteFile(filepath.Join(root, ".wippy.yaml"), []byte("version: '1.0'\noverride:\n  'acme.ui:policy:groups': [app:host_override]\n"), 0644))
				require.NoError(t, os.WriteFile(filepath.Join(root, "src", "host.yaml"), []byte("version: '1.0'\nnamespace: app\nentries:\n - name: dependency\n   kind: ns.dependency\n   component: acme/ui\n   parameters: [{name: group, value: 'app:host_selected'}]\n"), 0644))
				// Fresh command state exercises performPack's actual loader choice.
				command := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestModulePackCLIProcess$")
				command.Dir = root
				command.Env = []string{"WIPPY_PACK_TEST_HELPER=1", "WIPPY_PACK_TEST_OUTPUT=module.wapp", "WIPPY_PACK_TEST_CLOCK=2026-10-07T00:00:00Z", "WIPPY_PACK_TEST_MODULE=acme/ui", "HOME=" + root, "TMPDIR=" + root, "PATH=" + os.Getenv("PATH")}
				output, err := command.CombinedOutput()
				require.NoError(t, err, "%s", output)
			}
			file, err := os.Open(output)
			require.NoError(t, err)
			defer file.Close()
			reader, err := wapp.NewReader(file)
			require.NoError(t, err)
			rows, err := reader.GetEntries()
			require.NoError(t, err)
			for _, e := range rows {
				if e.ID.Name == "policy" {
					require.Equal(t, []any{"app:literal"}, e.Data.(map[string]any)["groups"])
				}
				if e.ID.Name == "group" {
					require.Equal(t, "app:members", e.Data.(map[string]any)["default"])
				}
				if e.ID.Name == "assets" {
					require.Equal(t, "fs.embed", e.Kind)
				}
			}
			assets, err := reader.GetFS(wapp.NewID("acme.ui", "assets"))
			require.NoError(t, err)
			content, err := fs.ReadFile(assets, "index.html")
			require.NoError(t, err)
			require.Equal(t, "module asset", string(content))
		})
	}
}
