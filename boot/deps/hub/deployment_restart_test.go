// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/boot/deps/lock"
	registryimpl "github.com/wippyai/runtime/system/registry"
	"github.com/wippyai/runtime/system/registry/expansion"
	historypg "github.com/wippyai/runtime/system/registry/history/postgres"
	historysqlite "github.com/wippyai/runtime/system/registry/history/sqlite"
	"github.com/wippyai/runtime/system/registry/topology"
	"github.com/wippyai/wapp"
	"go.uber.org/zap"
)

// The app owns a root-shaped dependency whose declaration changes when the
// updated app is materialized. Authored replay alone cannot reproduce it.
// This is the #889 cold-restart regression, without Keeper or production data.
func TestDeploymentOverlayColdRestart(t *testing.T) {
	for _, engine := range []string{"sqlite", "postgres"} {
		for _, legacy := range []bool{false, true} {
			for _, count := range []int{1, 75} {
				t.Run(fmt.Sprintf("%s/legacy=%t/roots=%d", engine, legacy, count), func(t *testing.T) {
					fixture := newDeploymentRestartFixtureWithModules(t, count)
					openHistory := deploymentRestartHistory(t, engine)
					history, closeHistory := openHistory()
					ctx, reg, _ := fixture.registryForModel(t, history, false, legacy)
					root, err := history.Head()
					require.NoError(t, err)
					require.NoError(t, reg.LoadState(ctx, fixture.baseline, root))
					initial := reg.Snapshot().Registry.Resolution

					var changes regapi.ChangeSet
					for _, module := range fixture.modules[1:] {
						entry, err := reg.GetEntry(regapi.NewID("app.deps", module))
						require.NoError(t, err)
						entry.Data = payload.New(map[string]any{"component": "acme/" + module, "version": ">=2.0.0"})
						changes = append(changes, regapi.Operation{Kind: regapi.EntryUpdate, Entry: entry})
					}
					_, err = reg.Apply(ctx, changes)
					require.NoError(t, err)
					head, err := reg.Apply(ctx, regapi.ChangeSet{{Kind: regapi.EntryCreate, Entry: regapi.Entry{
						ID: regapi.NewID("app.deps", "application"), Kind: regapi.NamespaceDependency,
						Data: payload.New(map[string]any{"component": "acme/app", "version": ">=2.0.0"}),
					}}})
					require.NoError(t, err)
					selected := reg.Snapshot().Registry.Resolution
					if legacy {
						require.NotEqual(t, initial.BaselineDigest, selected.BaselineDigest, "exercise the broken old digest model")
						require.Empty(t, selected.Deployment, "v0.3.44a does not record the deployment separately")
					} else {
						require.Equal(t, initial.BaselineDigest, selected.BaselineDigest,
							"Hub overlays must not change the deployment identity")
					}
					require.Equal(t, "2.0.0", resolutionModuleVersion(selected, "acme/app"))
					require.Equal(t, "2.0.0", resolutionModuleVersion(selected, "acme/worker"))
					closeHistory()

					lockBytes, err := os.ReadFile(fixture.lockPath)
					require.NoError(t, err)
					for i := 0; i < 2; i++ {
						history, closeHistory = openHistory()
						ctx, restarted, calls := fixture.registry(t, history, true)
						require.NoError(t, restarted.LoadState(ctx, fixture.baseline, head), "offline restart %d", i)
						require.Zero(t, *calls, "restart must not consult the Hub")
						restored := restarted.Snapshot().Registry.Resolution
						require.Equal(t, selected.Modules, restored.Modules, "exact versions and artifact digests must survive")
						require.Equal(t, selected.Roots, restored.Roots)
						require.Equal(t, selected.References, restored.References)
						if legacy && i == 0 {
							require.NotEqual(t, selected.BaselineDigest, restored.BaselineDigest)
							require.Equal(t, "acme/app", restored.Deployment.Root)
							for _, module := range restored.Deployment.Modules {
								require.Equal(t, "1.0.0", module.Version, "the deployment is not the updated graph")
							}
							selected = restored
						} else {
							require.Equal(t, selected, restored, "the corrected checkpoint must be restart-idempotent")
						}
						require.NoError(t, restarted.ApplyVersion(ctx, root))
						require.Equal(t, "1.0.0", snapshotModuleVersion(t, restarted, "acme/app"))
						require.NoError(t, restarted.ApplyVersion(ctx, head))
						require.Equal(t, selected, restarted.Snapshot().Registry.Resolution)
						closeHistory()
					}
					after, err := os.ReadFile(fixture.lockPath)
					require.NoError(t, err)
					require.Equal(t, lockBytes, after)
				})
			}
		}
	}
}

func TestDeploymentBaselineChangeKeepsHistoryOverlay(t *testing.T) {
	for _, engine := range []string{"sqlite", "postgres"} {
		for _, legacy := range []bool{false, true} {
			for _, input := range []string{"source", "source-masked-by-history", "source-with-later-owned-edit", "lock"} {
				t.Run(fmt.Sprintf("%s/legacy=%t/%s", engine, legacy, input), func(t *testing.T) {
					fixture := newDeploymentRestartFixture(t)
					openHistory := deploymentRestartHistory(t, engine)
					history, closeHistory := openHistory()
					ctx, reg, _ := fixture.registryForModel(t, history, false, legacy)
					root, err := history.Head()
					require.NoError(t, err)
					require.NoError(t, reg.LoadState(ctx, fixture.baseline, root))
					head := fixture.update(ctx, t, reg)
					if input == "source-with-later-owned-edit" {
						worker, err := reg.GetEntry(regapi.NewID("app.deps", "worker"))
						require.NoError(t, err)
						worker.Data = payload.New(map[string]any{"component": "acme/worker", "version": ">=2.0.0"})
						head, err = reg.Apply(ctx, regapi.ChangeSet{{Kind: regapi.EntryUpdate, Entry: worker}})
						require.NoError(t, err)
					}
					selected := reg.Snapshot().Registry.Resolution
					closeHistory()

					baseline := append(regapi.State(nil), fixture.baseline...)
					switch input {
					case "source", "source-with-later-owned-edit":
						version := ">=2.0.0"
						if input == "source-with-later-owned-edit" {
							version = ">=1.0.0"
						}
						baseline = append(baseline, regapi.Entry{
							ID: regapi.NewID("host.deps", "worker"), Kind: regapi.NamespaceDependency,
							Registry: regapi.EntryMetadata{Root: true},
							Data:     payload.New(map[string]any{"component": "acme/worker", "version": version}),
						})
					case "source-masked-by-history":
						for i := range baseline {
							if baseline[i].ID == regapi.NewID("app.deps", "worker") {
								baseline[i].Data = payload.New(map[string]any{"component": "acme/worker", "version": ">=1.1.0"})
							}
						}
					default:
						l, err := lock.New(fixture.lockPath)
						require.NoError(t, err)
						l.SetModule(lock.Module{Name: "acme/worker", Version: "2.0.0", Hash: strings.TrimPrefix(fixture.digests["worker@2.0.0"], "sha256:")})
						require.NoError(t, l.Write())
					}
					history, closeHistory = openHistory()
					ctx, restarted, calls := fixture.registry(t, history, false)
					require.NoError(t, restarted.LoadState(ctx, baseline, head))
					require.Positive(t, *calls, "a genuine deployment change must re-resolve")
					repaired := restarted.Snapshot().Registry.Resolution
					require.NotEqual(t, selected.BaselineDigest, repaired.BaselineDigest)
					require.Equal(t, "2.0.0", resolutionModuleVersion(repaired, "acme/app"), "the authored app overlay must remain applied")
					workerVersion := "2.0.0"
					if input == "source-masked-by-history" {
						// The app's later selection replaced the old Worker pin.
						// A real deployment refresh may select from the new baseline.
						workerVersion = "1.0.0"
					}
					require.Equal(t, workerVersion, resolutionModuleVersion(repaired, "acme/worker"))
					closeHistory()
					history, closeHistory = openHistory()
					ctx, restarted, calls = fixture.registry(t, history, true)
					require.NoError(t, restarted.LoadState(ctx, baseline, head))
					require.Zero(t, *calls)
					require.Equal(t, repaired, restarted.Snapshot().Registry.Resolution)
					closeHistory()
				})
			}
		}
	}
}

func TestRecoveredDeploymentBaselineNextHubUpdate(t *testing.T) {
	for _, engine := range []string{"sqlite", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			fixture := newDeploymentRestartFixture(t)
			openHistory := deploymentRestartHistory(t, engine)
			history, closeHistory := openHistory()
			ctx, reg, _ := fixture.registryForModel(t, history, false, true)
			root, err := history.Head()
			require.NoError(t, err)
			require.NoError(t, reg.LoadState(ctx, fixture.baseline, root))
			head := fixture.update(ctx, t, reg)
			closeHistory()
			history, closeHistory = openHistory()
			ctx, reg, calls := fixture.registry(t, history, false)
			require.NoError(t, reg.LoadState(ctx, fixture.baseline, head))
			require.Zero(t, *calls, "recovery must not ask the Hub to select versions")
			binding := reg.Snapshot().Registry.Resolution.BaselineDigest
			worker, err := reg.GetEntry(regapi.NewID("app.deps", "worker"))
			require.NoError(t, err)
			worker.Data = payload.New(map[string]any{
				"component": "acme/worker", "version": ">=2.0.0",
				"parameters": []any{map[string]any{"name": "scope", "value": "scope:new"}},
			})
			head, err = reg.Apply(ctx, regapi.ChangeSet{{Kind: regapi.EntryUpdate, Entry: worker}})
			require.NoError(t, err)
			selected := reg.Snapshot().Registry.Resolution
			require.Equal(t, binding, selected.BaselineDigest, "the first new update must retain the corrected deployment identity")
			state, err := reg.GetEntry(regapi.NewID("acme.worker", "state"))
			require.NoError(t, err)
			require.Equal(t, []any{"scope:new"}, state.Data.Data().(map[string]any)["groups"])
			closeHistory()
			history, closeHistory = openHistory()
			ctx, reg, calls = fixture.registry(t, history, true)
			require.NoError(t, reg.LoadState(ctx, fixture.baseline, head))
			require.Zero(t, *calls)
			require.Equal(t, selected, reg.Snapshot().Registry.Resolution)
			state, err = reg.GetEntry(regapi.NewID("acme.worker", "state"))
			require.NoError(t, err)
			require.Equal(t, []any{"scope:new"}, state.Data.Data().(map[string]any)["groups"],
				"later parameters must link once from the raw artifact, not revert to the owner's defaults")
			closeHistory()
		})
	}
}

func TestDeploymentRootAndOwnedUpdateInOneBatch(t *testing.T) {
	for _, appLast := range []bool{false, true} {
		t.Run(fmt.Sprintf("app-last=%t", appLast), func(t *testing.T) {
			fixture := newDeploymentRestartFixture(t)
			history, closeHistory := deploymentRestartHistory(t, "sqlite")()
			defer closeHistory()
			ctx, reg, _ := fixture.registry(t, history, false)
			root, err := history.Head()
			require.NoError(t, err)
			require.NoError(t, reg.LoadState(ctx, fixture.baseline, root))
			worker, err := reg.GetEntry(regapi.NewID("app.deps", "worker"))
			require.NoError(t, err)
			worker.Data = payload.New(map[string]any{"component": "acme/worker", "version": ">=2.0.0"})
			app := regapi.Entry{ID: regapi.NewID("app.deps", "application"), Kind: regapi.NamespaceDependency,
				Data: payload.New(map[string]any{"component": "acme/app", "version": ">=2.0.0"})}
			changes := regapi.ChangeSet{{Kind: regapi.EntryCreate, Entry: app}, {Kind: regapi.EntryUpdate, Entry: worker}}
			if appLast {
				changes[0], changes[1] = changes[1], changes[0]
			}
			head, err := reg.Apply(ctx, changes)
			if !appLast {
				// Preserve the existing live rejection of this combination;
				// changing batch precedence is a separate API decision.
				require.Error(t, err)
				current, headErr := history.Head()
				require.NoError(t, headErr)
				require.Equal(t, root.ID(), current.ID())
				return
			}
			require.NoError(t, err)
			selected := reg.Snapshot().Registry.Resolution
			ctx, reg, calls := fixture.registry(t, history, true)
			require.NoError(t, reg.LoadState(ctx, fixture.baseline, head))
			require.Zero(t, *calls)
			require.Equal(t, selected, reg.Snapshot().Registry.Resolution)
		})
	}
}

func TestDeploymentMixedBatchPreservesColdParameterPrecedence(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(fmt.Sprintf("legacy=%t", legacy), func(t *testing.T) {
			fixture := newDeploymentRestartFixture(t)
			history, closeHistory := deploymentRestartHistory(t, "sqlite")()
			defer closeHistory()
			ctx, reg, _ := fixture.registryForModel(t, history, false, legacy)
			root, err := history.Head()
			require.NoError(t, err)
			require.NoError(t, reg.LoadState(ctx, fixture.baseline, root))
			worker, err := reg.GetEntry(regapi.NewID("app.deps", "worker"))
			require.NoError(t, err)
			worker.Data = payload.New(map[string]any{
				"component": "acme/worker", "version": "*",
				"parameters": []any{map[string]any{"name": "scope", "value": "scope:overridden"}},
			})
			head, err := reg.Apply(ctx, regapi.ChangeSet{
				{Kind: regapi.EntryUpdate, Entry: worker},
				{Kind: regapi.EntryCreate, Entry: regapi.Entry{
					ID: regapi.NewID("app.deps", "application"), Kind: regapi.NamespaceDependency,
					Data: payload.New(map[string]any{"component": "acme/app", "version": ">=2.0.0"}),
				}},
			})
			require.NoError(t, err)
			before, err := reg.GetEntry(regapi.NewID("acme.worker", "state"))
			require.NoError(t, err)
			require.Equal(t, []any{"scope:default"}, before.Data.Data().(map[string]any)["groups"])
			ctx, reg, calls := fixture.registry(t, history, true)
			require.NoError(t, reg.LoadState(ctx, fixture.baseline, head))
			require.Zero(t, *calls)
			after, err := reg.GetEntry(before.ID)
			require.NoError(t, err)
			require.Equal(t, before.Data.Data(), after.Data.Data(),
				"sorted journal order must not resurrect a same-batch parameter overwritten by the owner update")
		})
	}
}

func (f *deploymentRestartFixture) update(ctx context.Context, t *testing.T, reg *registryimpl.Reg) regapi.Version {
	t.Helper()
	worker, err := reg.GetEntry(regapi.NewID("app.deps", "worker"))
	require.NoError(t, err)
	worker.Data = payload.New(map[string]any{"component": "acme/worker", "version": ">=2.0.0"})
	_, err = reg.Apply(ctx, regapi.ChangeSet{{Kind: regapi.EntryUpdate, Entry: worker}})
	require.NoError(t, err)
	head, err := reg.Apply(ctx, regapi.ChangeSet{{Kind: regapi.EntryCreate, Entry: regapi.Entry{
		ID: regapi.NewID("app.deps", "application"), Kind: regapi.NamespaceDependency,
		Data: payload.New(map[string]any{"component": "acme/app", "version": ">=2.0.0"}),
	}}})
	require.NoError(t, err)
	return head
}

type deploymentRestartFixture struct {
	lockPath  string
	vendorDir string
	baseline  regapi.State
	artifacts map[string][]byte
	digests   map[string]string
	modules   []string
}

func newDeploymentRestartFixture(t *testing.T) *deploymentRestartFixture {
	t.Helper()
	return newDeploymentRestartFixtureWithModules(t, 1)
}

func newDeploymentRestartFixtureWithModules(t *testing.T, count int) *deploymentRestartFixture {
	t.Helper()
	root := t.TempDir()
	f := &deploymentRestartFixture{
		lockPath: filepath.Join(root, "wippy.lock"), vendorDir: filepath.Join(root, "vendor"),
		artifacts: make(map[string][]byte), digests: make(map[string]string),
		modules: []string{"app", "worker"},
	}
	for i := 1; i < count; i++ {
		f.modules = append(f.modules, fmt.Sprintf("worker%d", i))
	}
	for _, module := range f.modules {
		for _, version := range []string{"1.0.0", "2.0.0"} {
			entries := []wapp.Entry{
				{ID: wapp.NewID("acme."+module, "definition"), Kind: regapi.NamespaceDefinition},
				{ID: wapp.NewID("acme."+module, "state"), Kind: regapi.EntryKind, Data: map[string]any{"generation": version}},
			}
			if module == "worker" {
				entries[1].Data.(map[string]any)["groups"] = []any{}
				entries = append(entries, wapp.Entry{
					ID: wapp.NewID("acme.worker", "scope"), Kind: regapi.NamespaceRequirement,
					Data: map[string]any{
						"default": "scope:default",
						"targets": []any{map[string]any{"entry": "state", "path": ".groups +="}},
					},
				})
			}
			if module == "app" {
				constraint := "*"
				if version == "1.0.0" {
					constraint = ">=1.0.0"
				}
				for _, dependency := range f.modules[1:] {
					entries = append(entries, wapp.Entry{
						ID: wapp.NewID("app.deps", dependency), Kind: regapi.NamespaceDependency,
						Data: map[string]any{"component": "acme/" + dependency, "version": constraint},
					})
				}
			}
			key := module + "@" + version
			f.artifacts[key] = buildWappBytes(t, entries)
			sum := sha256.Sum256(f.artifacts[key])
			f.digests[key] = "sha256:" + hex.EncodeToString(sum[:])
			if version == "1.0.0" {
				for _, e := range entries {
					f.baseline = append(f.baseline, regapi.Entry{
						ID: regapi.NewID(e.ID.Namespace, e.ID.Name), Kind: e.Kind, Data: payload.New(e.Data),
						Registry: regapi.EntryMetadata{Owner: "acme/" + module, Root: e.Kind == regapi.NamespaceDependency},
					})
				}
			}
		}
	}
	l, err := lock.New(f.lockPath)
	require.NoError(t, err)
	l.SetDirectories(lock.Directories{Modules: "vendor"})
	for _, module := range f.modules {
		l.SetModule(lock.Module{Name: "acme/" + module, Version: "1.0.0", Root: module == "app",
			Hash: strings.TrimPrefix(f.digests[module+"@1.0.0"], "sha256:")})
		require.NoError(t, os.MkdirAll(filepath.Join(f.vendorDir, "acme"), 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(f.vendorDir, "acme", module+"-1.0.0.wapp"), f.artifacts[module+"@1.0.0"], 0o600))
	}
	require.NoError(t, l.Write())
	return f
}

func (f *deploymentRestartFixture) registry(t *testing.T, history regapi.History, offline bool) (context.Context, *registryimpl.Reg, *int) {
	t.Helper()
	return f.registryForModel(t, history, offline, false)
}

func (f *deploymentRestartFixture) registryForModel(t *testing.T, history regapi.History, offline, legacy bool) (context.Context, *registryimpl.Reg, *int) {
	t.Helper()
	return f.registryWithRunner(t, history, offline, legacy, &bootRecordingRunner{})
}

func (f *deploymentRestartFixture) registryWithRunner(t *testing.T, history regapi.History, offline, legacy bool, runner regapi.Runner) (context.Context, *registryimpl.Reg, *int) {
	t.Helper()
	calls := 0
	client := &fakeHub{
		getManifest: func(_ context.Context, org, module, version string) (*ModuleManifest, error) {
			calls++
			if offline {
				return nil, fmt.Errorf("unexpected offline manifest request")
			}
			key := module + "@" + version
			blob, ok := f.artifacts[key]
			if !ok {
				return nil, fmt.Errorf("unexpected manifest %s", key)
			}
			manifest := &ModuleManifest{Org: org, Name: module, Version: version, VersionID: version,
				Digest: f.digests[key], SizeBytes: uint64(len(blob)), URL: "fixture://" + key}
			if module == "app" {
				for _, dependency := range f.modules[1:] {
					manifest.Dependencies = append(manifest.Dependencies, ManifestDep{Org: "acme", Name: dependency, Constraint: "*", Version: version})
				}
			}
			return manifest, nil
		},
		listVersions: func(context.Context, string, string) ([]VersionInfo, error) {
			calls++
			if offline {
				return nil, fmt.Errorf("unexpected offline versions request")
			}
			return []VersionInfo{{ID: "1.0.0", Version: "1.0.0"}, {ID: "2.0.0", Version: "2.0.0"}}, nil
		},
		downloadFile: func(_ context.Context, url, dest string) error {
			calls++
			if offline {
				return fmt.Errorf("unexpected offline artifact request")
			}
			blob, ok := f.artifacts[strings.TrimPrefix(url, "fixture://")]
			if !ok {
				return fmt.Errorf("unexpected artifact %s", url)
			}
			if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
				return err
			}
			return os.WriteFile(dest, blob, 0o600)
		},
	}
	ctx := newTestContext()
	access := regapi.DependencyAccessOnline
	if offline {
		access = regapi.DependencyAccessVerifiedOffline
	}
	ctx = regapi.WithDependencyAccess(ctx, access)
	resolver := topology.NewResolver()
	handler, err := NewDependencyHandler(DependencyHandlerOptions{Hub: client, Resolver: resolver, Logger: zap.NewNop(), LockPath: f.lockPath, VendorDir: f.vendorDir})
	require.NoError(t, err)
	t.Cleanup(handler.manifestCache.Close)
	require.NoError(t, handler.PrepareRestore(ctx, history))
	expand := handler.Expand
	expandChanges := handler.ExpandChanges
	if legacy {
		legacyResult := func(ctx context.Context, result regapi.DirectiveResult, changes regapi.ChangeSet, snapshot regapi.State, err error) (regapi.DirectiveResult, error) {
			if err != nil || result.Resolution == nil {
				return result, err
			}
			state := snapshot
			for _, op := range changes[:len(changes)-1] {
				state = applyOperationToState(state, op)
			}
			for _, additional := range result.Additional {
				state = applyOperationToState(state, additional.Operation)
			}
			state = applyOperationToState(state, changes[len(changes)-1])
			oldWriter := *handler
			oldWriter.deployment = nil // v0.3.44a hashes the original lock fields.
			digest, err := oldWriter.hashDeploymentBaseline(ctx, state, payload.GetTranscoder(ctx), "deployment-overlay-v1")
			if err != nil {
				return regapi.DirectiveResult{}, err
			}
			result.Resolution.BaselineDigest = digest
			result.Resolution.Deployment = nil
			result.Resolution = result.Resolution.Canonical()
			return result, nil
		}
		expand = func(ctx context.Context, op regapi.Operation, snapshot regapi.State) (regapi.DirectiveResult, error) {
			result, err := handler.Expand(ctx, op, snapshot)
			return legacyResult(ctx, result, regapi.ChangeSet{op}, snapshot, err)
		}
		expandChanges = func(ctx context.Context, changes regapi.ChangeSet, snapshot regapi.State) (regapi.DirectiveResult, error) {
			result, err := handler.ExpandChanges(ctx, changes, snapshot)
			return legacyResult(ctx, result, changes, snapshot, err)
		}
	}
	reg := registryimpl.NewRegistry(history, runner, topology.NewStateBuilder(zap.NewNop(), resolver), resolver, zap.NewNop(),
		registryimpl.WithKindDirective(regapi.NamespaceDependency, expansion.NewDependencyDirective(expand).
			WithChangesExpansion(expandChanges).WithResolutionTransition(handler.ReconcileResolution)))
	return regapi.WithRegistry(ctx, reg), reg, &calls
}

func deploymentRestartHistory(t *testing.T, engine string) func() (regapi.History, func()) {
	t.Helper()
	if engine == "sqlite" {
		path := filepath.Join(t.TempDir(), "registry.db")
		return func() (regapi.History, func()) {
			h, err := historysqlite.NewSQLite(path, zap.NewNop())
			require.NoError(t, err)
			return h, func() { require.NoError(t, h.Close()) }
		}
	}
	dsn := os.Getenv("WIPPY_POSTGRES_HISTORY_TEST_DSN")
	if strings.TrimSpace(dsn) == "" {
		t.Skip("WIPPY_POSTGRES_HISTORY_TEST_DSN is not set")
	}
	schema := fmt.Sprintf("wippy_deployment_restart_%d", time.Now().UnixNano())
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, err := db.ExecContext(context.Background(), fmt.Sprintf("DROP SCHEMA IF EXISTS %q CASCADE", schema))
		require.NoError(t, err)
		require.NoError(t, db.Close())
	})
	return func() (regapi.History, func()) {
		h, err := historypg.NewPostgres(dsn, schema, zap.NewNop())
		require.NoError(t, err)
		return h, func() { require.NoError(t, h.Close()) }
	}
}
