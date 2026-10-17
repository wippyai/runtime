// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/boot/build/stages"
	"github.com/wippyai/runtime/boot/deps/lock"
	"github.com/wippyai/wapp"
)

func TestRootUpdateRelinksUnchangedDependencyParameters(t *testing.T) {
	for _, engine := range []string{"sqlite", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			root := t.TempDir()
			fixture := &deploymentRestartFixture{
				lockPath: filepath.Join(root, "wippy.lock"), vendorDir: filepath.Join(root, "vendor"),
				modules: []string{"app", "worker"}, artifacts: map[string][]byte{}, digests: map[string]string{},
			}
			worker := []wapp.Entry{{ID: wapp.NewID("acme.worker", "definition"), Kind: regapi.NamespaceDefinition}}
			for _, name := range []string{"removed", "changed", "added"} {
				worker = append(worker, wapp.Entry{ID: wapp.NewID("acme.worker", name), Kind: regapi.NamespaceRequirement,
					Data: map[string]any{"default": "package:default", "targets": []any{map[string]any{"entry": name, "path": ".default"}}}})
			}
			for _, version := range []string{"1.0.0", "2.0.0"} {
				params := []Parameter{{Name: "removed", Value: "host:removed"}, {Name: "changed", Value: "host:before"}}
				if version == "2.0.0" {
					params = []Parameter{{Name: "changed", Value: "host:after"}, {Name: "added", Value: "host:added"}}
				}
				app := []wapp.Entry{
					{ID: wapp.NewID("acme.app", "definition"), Kind: regapi.NamespaceDefinition},
					{ID: wapp.NewID("app.deps", "worker"), Kind: regapi.NamespaceDependency,
						Data: DependencyDefinition{Component: "acme/worker", Version: "1.0.0", Parameters: params}},
				}
				for module, entries := range map[string][]wapp.Entry{"app": app, "worker": worker} {
					key := module + "@" + version
					fixture.artifacts[key] = buildWappBytes(t, entries)
					sum := sha256.Sum256(fixture.artifacts[key])
					fixture.digests[key] = "sha256:" + hex.EncodeToString(sum[:])
					if version == "1.0.0" {
						for _, entry := range entries {
							fixture.baseline = append(fixture.baseline, regapi.Entry{
								ID: regapi.NewID(entry.ID.Namespace, entry.ID.Name), Kind: entry.Kind, Data: payload.New(entry.Data),
								Registry: regapi.EntryMetadata{Owner: "acme/" + module, Root: entry.Kind == regapi.NamespaceDependency},
							})
						}
					}
				}
			}
			l, err := lock.New(fixture.lockPath)
			require.NoError(t, err)
			l.SetDirectories(lock.Directories{Modules: "vendor"})
			for _, module := range fixture.modules {
				l.SetModule(lock.Module{Name: "acme/" + module, Version: "1.0.0", Root: module == "app",
					Hash: strings.TrimPrefix(fixture.digests[module+"@1.0.0"], "sha256:")})
				require.NoError(t, os.MkdirAll(filepath.Join(fixture.vendorDir, "acme"), 0o755))
				require.NoError(t, os.WriteFile(filepath.Join(fixture.vendorDir, "acme", module+"-1.0.0.wapp"), fixture.artifacts[module+"@1.0.0"], 0o600))
			}
			require.NoError(t, l.Write())
			baseline := []regapi.Entry(fixture.baseline)
			require.NoError(t, stages.Link().Execute(newTestContext(), &baseline))
			fixture.baseline = baseline
			openHistory := deploymentRestartHistory(t, engine)
			history, closeHistory := openHistory()
			ctx, live, _ := fixture.registry(t, history, false)
			initial, err := history.Head()
			require.NoError(t, err)
			require.NoError(t, live.LoadState(ctx, fixture.baseline, initial))
			head, err := live.Apply(ctx, regapi.ChangeSet{{Kind: regapi.EntryCreate, Entry: regapi.Entry{
				ID: regapi.NewID("host.deps", "application"), Kind: regapi.NamespaceDependency,
				Data: payload.New(DependencyDefinition{Component: "acme/app", Version: "2.0.0"}),
			}}})
			require.NoError(t, err)
			check := func(reg regapi.Registry, expected map[string]string) {
				for name, want := range expected {
					entry, err := reg.GetEntry(regapi.NewID("acme.worker", name))
					require.NoError(t, err)
					require.Equal(t, want, entry.Data.Data().(map[string]any)["default"], name)
				}
			}
			after := map[string]string{"removed": "package:default", "changed": "host:after", "added": "host:added"}
			check(live, after)
			require.Equal(t, "1.0.0", snapshotModuleVersion(t, live, "acme/worker"))
			closeHistory()
			history, closeHistory = openHistory()
			defer closeHistory()
			ctx, restored, calls := fixture.registry(t, history, true)
			require.NoError(t, restored.LoadState(ctx, fixture.baseline, head))
			require.Zero(t, *calls)
			check(restored, after)
			require.NoError(t, restored.ApplyVersion(ctx, initial))
			check(restored, map[string]string{"removed": "host:removed", "changed": "host:before", "added": "package:default"})
			require.NoError(t, restored.ApplyVersion(ctx, head))
			check(restored, after)
		})
	}
}
