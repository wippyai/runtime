// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/boot/deps/lock"
	"go.uber.org/zap"
)

// TestWorkspaceReplacementStateMachine exercises the workspace resolver as a
// persisted state machine, rather than as isolated calls. Every seed mixes
// source-root changes, replacement configuration changes, tree drift, missing
// paths, full/targeted updates, repeated updates, and lock-file restarts. A
// failing seed prints the complete operation trace so it is reproducible.
func TestWorkspaceReplacementStateMachine(t *testing.T) {
	const (
		localModule  = "local/component"
		remoteModule = "acme/remote"
		transitive   = "acme/runtime"
	)

	for seed := int64(0); seed < 16; seed++ {
		t.Run(fmt.Sprintf("seed-%02d", seed), func(t *testing.T) {
			rng := rand.New(rand.NewSource(seed))
			root := t.TempDir()
			lockPath := filepath.Join(root, lock.DefaultFilename)
			replacementPath := filepath.Join(root, "local-component")
			parkedPath := replacementPath + ".missing"
			require.NoError(t, os.MkdirAll(replacementPath, 0o755))

			provider := &fakeHub{
				listVersions: func(_ context.Context, org, module string) ([]VersionInfo, error) {
					name := org + "/" + module
					switch name {
					case localModule, remoteModule, transitive:
						versions := []VersionInfo{{Version: "1.0.0"}}
						if name == transitive {
							versions = append(versions, VersionInfo{Version: "2.0.0"})
						}
						return versions, nil
					default:
						return nil, fmt.Errorf("unexpected module %s", name)
					}
				},
				getManifest: func(_ context.Context, org, module, constraint string) (*ModuleManifest, error) {
					name := org + "/" + module
					valid := constraint == "1.0.0" || (name == transitive && constraint == "2.0.0")
					if !valid {
						return nil, fmt.Errorf("manifest %s@%s not found", name, constraint)
					}
					digestKey := name
					if name == transitive {
						digestKey += "@" + constraint
					}
					return &ModuleManifest{
						Org: org, Name: module, Version: constraint,
						Digest: "sha256:" + strings.Repeat(map[string]string{
							localModule: "a", remoteModule: "b", transitive + "@1.0.0": "c", transitive + "@2.0.0": "d",
						}[digestKey], 64),
					}, nil
				},
			}

			lockObj, err := lock.New(lockPath)
			require.NoError(t, err)
			lockObj.SetDirectories(lock.Directories{Modules: ".wippy", Src: "src"})
			lockObj.SetReplacement(lock.Replacement{From: localModule, To: replacementPath})
			require.NoError(t, lockObj.Write())

			localRoot, remoteRoot := true, true
			replacementConfigured, replacementAvailable := true, true
			transitiveVersion := "1.0.0"
			trace := make([]string, 0, 48)

			writeReplacement := func() {
				t.Helper()
				content := fmt.Sprintf(`namespace: local.component
entries:
  - name: runtime
    kind: ns.dependency
    component: acme/runtime
    version: %s
`, transitiveVersion)
				require.NoError(t, os.WriteFile(filepath.Join(replacementPath, "_index.yaml"), []byte(content), 0o600))
			}
			writeReplacement()

			fail := func(step int, format string, args ...any) {
				t.Helper()
				t.Fatalf("seed=%d step=%d: %s\ntrace:\n  %s", seed, step, fmt.Sprintf(format, args...), strings.Join(trace, "\n  "))
			}

			for step := 0; step < 40; step++ {
				op := rng.Intn(11)
				switch op {
				case 0:
					localRoot = !localRoot
					trace = append(trace, fmt.Sprintf("%02d toggle local root -> %t", step, localRoot))
				case 1:
					remoteRoot = !remoteRoot
					trace = append(trace, fmt.Sprintf("%02d toggle remote root -> %t", step, remoteRoot))
				case 2:
					replacementConfigured = !replacementConfigured
					if replacementConfigured {
						lockObj.SetReplacement(lock.Replacement{From: localModule, To: replacementPath})
					} else {
						lockObj.RemoveReplacement(localModule)
					}
					trace = append(trace, fmt.Sprintf("%02d toggle replacement config -> %t", step, replacementConfigured))
				case 3:
					if replacementAvailable {
						require.NoError(t, os.Rename(replacementPath, parkedPath))
					} else {
						require.NoError(t, os.Rename(parkedPath, replacementPath))
					}
					replacementAvailable = !replacementAvailable
					trace = append(trace, fmt.Sprintf("%02d toggle replacement path -> %t", step, replacementAvailable))
				case 4:
					transitiveVersion = map[string]string{"1.0.0": "2.0.0", "2.0.0": "1.0.0"}[transitiveVersion]
					if replacementAvailable {
						writeReplacement()
					} else {
						content := fmt.Sprintf("namespace: local.component\nentries:\n  - name: runtime\n    kind: ns.dependency\n    component: acme/runtime\n    version: %s\n", transitiveVersion)
						require.NoError(t, os.WriteFile(filepath.Join(parkedPath, "_index.yaml"), []byte(content), 0o600))
					}
					trace = append(trace, fmt.Sprintf("%02d replacement dependency -> %s", step, transitiveVersion))
				case 5:
					trace = append(trace, fmt.Sprintf("%02d restart from persisted lock", step))
				case 6:
					trace = append(trace, fmt.Sprintf("%02d full update", step))
				case 7:
					trace = append(trace, fmt.Sprintf("%02d targeted remote update", step))
				case 8:
					trace = append(trace, fmt.Sprintf("%02d lock roundtrip", step))
				case 9:
					trace = append(trace, fmt.Sprintf("%02d repeated resolution", step))
				case 10:
					lockObj.SetModule(lock.Module{Name: "history/only", Version: "9.9.9", Hash: strings.Repeat("e", 64)})
					trace = append(trace, fmt.Sprintf("%02d inject history-only stale lock row", step))
				}

				require.NoError(t, lockObj.Write())
				lockObj, err = lock.New(lockPath)
				if err != nil {
					fail(step, "reload lock: %v", err)
				}

				roots := make([]DependencyDefinition, 0, 2)
				if localRoot {
					roots = append(roots, DependencyDefinition{Component: localModule, Version: "*"})
				}
				if remoteRoot {
					roots = append(roots, DependencyDefinition{Component: remoteModule, Version: "1.0.0"})
				}
				handler, handlerErr := NewDependencyHandler(DependencyHandlerOptions{
					Hub: provider, Logger: zap.NewNop(), LockPath: lockPath,
				})
				if handlerErr != nil {
					fail(step, "construct handler: %v", handlerErr)
				}
				expectedLocalVersion := "1.0.0"
				if replacementConfigured {
					expectedLocalVersion = replacementZeroVersion
					if selected, ok := lockObj.GetModule(localModule); ok {
						expectedLocalVersion = selected.Version
					}
				}

				var resolved []ResolvedModule
				switch op {
				case 0, 1, 2, 6:
					resolved, err = handler.UpdateWorkspaceDependencies(newTestContext(), roots, nil)
				case 7:
					resolved, err = handler.UpdateWorkspaceDependencies(newTestContext(), roots, []string{remoteModule})
				default:
					resolved, err = handler.ResolveWorkspaceDependencies(newTestContext(), roots)
				}

				requiredMissing := localRoot && replacementConfigured && !replacementAvailable
				if requiredMissing {
					if err == nil || !strings.Contains(err.Error(), localModule) || !strings.Contains(err.Error(), replacementPath) {
						fail(step, "required missing replacement error = %v; want module and path", err)
					}
					continue
				}
				if err != nil {
					fail(step, "resolution failed: %v", err)
				}

				modules := make([]lock.Module, 0, len(resolved))
				actual := make([]string, 0, len(resolved))
				for _, module := range resolved {
					name := module.Org + "/" + module.Name
					actual = append(actual, name+"@"+module.Version)
					modules = append(modules, lock.Module{Name: name, Version: module.Version, Hash: module.Digest})
				}
				lockObj.ReplaceModules(modules)
				require.NoError(t, lockObj.Write())
				lockObj, err = lock.New(lockPath)
				if err != nil {
					fail(step, "reload resolved lock: %v", err)
				}

				expected := make([]string, 0, 3)
				if localRoot {
					if replacementConfigured {
						expected = append(expected, localModule+"@"+expectedLocalVersion, transitive+"@"+transitiveVersion)
					} else {
						expected = append(expected, localModule+"@1.0.0")
					}
				}
				if remoteRoot {
					expected = append(expected, remoteModule+"@1.0.0")
				}
				sort.Strings(actual)
				sort.Strings(expected)
				if strings.Join(actual, ",") != strings.Join(expected, ",") {
					fail(step, "selected graph = %v; want %v", actual, expected)
				}

				replacementLoads := 0
				for _, loadPath := range lockObj.GetModuleLoadPaths() {
					if loadPath.Replacement {
						replacementLoads++
						if loadPath.Module != localModule || !localRoot || !replacementConfigured {
							fail(step, "unexpected replacement load path: %+v", loadPath)
						}
					}
				}
				wantReplacementLoads := 0
				if localRoot && replacementConfigured {
					wantReplacementLoads = 1
				}
				if replacementLoads != wantReplacementLoads {
					fail(step, "replacement load paths = %d; want %d", replacementLoads, wantReplacementLoads)
				}
			}
		})
	}
}
