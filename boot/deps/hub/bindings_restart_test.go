// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/boot/build/stages"
	"github.com/wippyai/runtime/boot/deps/lock"
	"github.com/wippyai/runtime/internal/version"
	"github.com/wippyai/wapp"
)

func TestRecordedBatchBindingsSurviveRestartAndUndoRedo(t *testing.T) {
	for _, engine := range []string{"sqlite", "postgres"} {
		for _, appLast := range []bool{false, true} {
			for _, qualified := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/app-last=%t/qualified=%t", engine, appLast, qualified), func(t *testing.T) {
					fixture := newBindingRestartFixture(t)
					openHistory := deploymentRestartHistory(t, engine)
					history, closeHistory := openHistory()
					ctx, live, _ := fixture.registry(t, history, false)
					root, err := history.Head()
					require.NoError(t, err)
					require.NoError(t, live.LoadState(ctx, fixture.baseline, root))
					worker, err := live.GetEntry(regapi.NewID("app.deps", "worker"))
					require.NoError(t, err)
					name := "scope"
					if qualified {
						name = "acme.worker:scope"
					}
					worker.Data = payload.New(DependencyDefinition{Component: "acme/worker", Version: "2.0.0", Parameters: []Parameter{
						{Name: name, Value: "scope:overridden"}, {Name: "enabled", Value: true}, {Name: "limit", Value: 10},
					}})
					app := regapi.Entry{ID: regapi.NewID("app.deps", "application"), Kind: regapi.NamespaceDependency,
						Data: payload.New(DependencyDefinition{Component: "acme/app", Version: ">=2.0.0"})}
					changes := regapi.ChangeSet{{Kind: regapi.EntryCreate, Entry: app}, {Kind: regapi.EntryUpdate, Entry: worker}}
					if appLast {
						changes[0], changes[1] = changes[1], changes[0]
					}
					planner := regapi.Planner(live)
					plan, err := planner.Plan(ctx, root, changes)
					require.NoError(t, err)
					head, err := live.ApplyPlan(ctx, plan)
					require.NoError(t, err, "planning and applying must use identical durable bindings")
					selected := live.Snapshot().Registry.Resolution
					require.NotEmpty(t, selected.Bindings)
					before, err := live.GetEntry(regapi.NewID("acme.worker", "state"))
					require.NoError(t, err)
					require.Equal(t, []any{"scope:overridden"}, before.Data.Data().(map[string]any)["groups"])
					require.Equal(t, true, before.Meta["enabled"])
					require.EqualValues(t, 10, before.Meta["limit"])
					require.EqualValues(t, 42, before.Meta["introduced"])
					authored, err := live.GetEntry(worker.ID)
					require.NoError(t, err)
					definition, err := decodeDependency(ctx, payload.GetTranscoder(ctx), authored)
					require.NoError(t, err)
					require.Len(t, definition.Parameters, 3, "artifact defaults must not be written into the authored entry")
					require.Equal(t, name, definition.Parameters[0].Name, "authored aliases retain their spelling")
					want := bindingEntryJSON(t, before)
					closeHistory()

					for restart := 0; restart < 2; restart++ {
						history, closeHistory = openHistory()
						ctx, restored, calls := fixture.registry(t, history, true)
						require.NoError(t, restored.LoadState(ctx, fixture.baseline, head))
						require.Zero(t, *calls)
						require.Equal(t, selected, restored.Snapshot().Registry.Resolution)
						after, err := restored.GetEntry(before.ID)
						require.NoError(t, err)
						require.Equal(t, want, bindingEntryJSON(t, after))
						require.NoError(t, restored.ApplyVersion(ctx, root))
						require.NoError(t, restored.ApplyVersion(ctx, head))
						after, err = restored.GetEntry(before.ID)
						require.NoError(t, err)
						require.Equal(t, want, bindingEntryJSON(t, after), "undo/redo must retain exact effective bindings")
						closeHistory()
					}
				})
			}
		}
	}
}

func TestLegacyMixedBatchDoesNotGainModernBindingPrecedence(t *testing.T) {
	for _, engine := range []string{"sqlite", "postgres"} {
		for _, legacyDigest := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/legacy-digest=%t", engine, legacyDigest), func(t *testing.T) {
				fixture := newDeploymentRestartFixture(t)
				history, closeHistory := deploymentRestartHistory(t, engine)()
				ctx, live, _ := fixture.registryForModel(t, history, false, legacyDigest)
				root, err := history.Head()
				require.NoError(t, err)
				require.NoError(t, live.LoadState(ctx, fixture.baseline, root))
				worker, err := live.GetEntry(regapi.NewID("app.deps", "worker"))
				require.NoError(t, err)
				worker.Data = payload.New(DependencyDefinition{Component: "acme/worker", Version: "*",
					Parameters: []Parameter{{Name: "scope", Value: "scope:historically-overwritten"}}})
				head, err := live.Apply(ctx, regapi.ChangeSet{
					{Kind: regapi.EntryUpdate, Entry: worker},
					{Kind: regapi.EntryCreate, Entry: regapi.Entry{ID: regapi.NewID("app.deps", "application"), Kind: regapi.NamespaceDependency,
						Data: payload.New(DependencyDefinition{Component: "acme/app", Version: ">=2.0.0"})}},
				})
				require.NoError(t, err)
				changes, err := history.Get(head)
				require.NoError(t, err)
				oldGraph := live.Snapshot().Registry.Resolution.Canonical()
				oldGraph.Bindings = nil // The pre-#954 writer never recorded binding precedence.
				oldGraph = oldGraph.Canonical()
				closeHistory()

				openLegacyHistory := deploymentRestartHistory(t, engine)
				history, closeHistory = openLegacyHistory()
				legacyRoot, err := history.Head()
				require.NoError(t, err)
				oldHead := version.FromParent(legacyRoot, 1)
				durable, ok := history.(regapi.ResolutionHistory)
				require.True(t, ok)
				require.NoError(t, durable.SaveWithDependencyResolution(oldHead, changes, oldGraph, true))
				ctx, restored, calls := fixture.registry(t, history, true)
				require.NoError(t, restored.LoadState(ctx, fixture.baseline, oldHead))
				require.Zero(t, *calls)
				after, err := restored.GetEntry(regapi.NewID("acme.worker", "state"))
				require.NoError(t, err)
				require.Equal(t, []any{"scope:default"}, after.Data.Data().(map[string]any)["groups"], "an unannotated old batch retains its historical artifact precedence")
				require.Empty(t, restored.Snapshot().Registry.Resolution.Bindings)
				// A new, unrelated graph must not promote an old ambiguous edit
				// to modern precedence merely because the snapshot is newer.
				next, err := restored.Apply(ctx, regapi.ChangeSet{{Kind: regapi.EntryCreate, Entry: regapi.Entry{
					ID: regapi.NewID("host.deps", "alias"), Kind: regapi.NamespaceDependency,
					Data: payload.New(DependencyDefinition{Component: "acme/worker", Version: "1.0.0"}),
				}}})
				require.NoError(t, err)
				ctx, again, _ := fixture.registry(t, history, true)
				require.NoError(t, again.LoadState(ctx, fixture.baseline, next))
				after, err = again.GetEntry(regapi.NewID("acme.worker", "state"))
				require.NoError(t, err)
				require.Equal(t, []any{"scope:default"}, after.Data.Data().(map[string]any)["groups"])
				closeHistory()
			})
		}
	}
}

func TestLegacyGraphlessBatchRestoreKeepsOldPrecedence(t *testing.T) {
	for _, engine := range []string{"sqlite", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			fixture := newDeploymentRestartFixture(t)
			// Real boot links pristine source entries before registry replay.
			// Graph-less recovery may reuse unchanged baseline modules, so use
			// that linked baseline rather than the fixture's raw artifact state.
			baseline := []regapi.Entry(fixture.baseline)
			require.NoError(t, stages.Link().Execute(newTestContext(), &baseline))
			fixture.baseline = baseline
			history, closeHistory := deploymentRestartHistory(t, engine)()
			ctx, live, _ := fixture.registry(t, history, false)
			root, err := history.Head()
			require.NoError(t, err)
			require.NoError(t, live.LoadState(ctx, fixture.baseline, root))
			worker, err := live.GetEntry(regapi.NewID("app.deps", "worker"))
			require.NoError(t, err)
			worker.Data = payload.New(DependencyDefinition{Component: "acme/worker", Version: "*", Parameters: []Parameter{{Name: "scope", Value: "scope:old-edit"}}})
			head, err := live.Apply(ctx, regapi.ChangeSet{
				{Kind: regapi.EntryUpdate, Entry: worker},
				{Kind: regapi.EntryCreate, Entry: regapi.Entry{ID: regapi.NewID("app.deps", "application"), Kind: regapi.NamespaceDependency,
					Data: payload.New(DependencyDefinition{Component: "acme/app", Version: ">=2.0.0"})}},
			})
			require.NoError(t, err)
			changes, err := history.Get(head)
			require.NoError(t, err)
			closeHistory()
			history, closeHistory = deploymentRestartHistory(t, engine)()
			defer closeHistory()
			root, err = history.Head()
			require.NoError(t, err)
			oldHead := version.FromParent(root, 1)
			require.NoError(t, history.Save(oldHead, changes, true), "simulate a writer predating graph checkpoints")
			ctx, restored, _ := fixture.registry(t, history, false)
			require.NoError(t, restored.LoadState(ctx, fixture.baseline, oldHead))
			require.Empty(t, restored.Snapshot().Registry.Resolution.Bindings, "historical replay is not a fresh explicit operation")
			state, err := restored.GetEntry(regapi.NewID("acme.worker", "state"))
			require.NoError(t, err)
			require.Equal(t, []any{"scope:default"}, state.Data.Data().(map[string]any)["groups"])
			ctx, again, calls := fixture.registry(t, history, true)
			require.NoError(t, again.LoadState(ctx, fixture.baseline, oldHead))
			require.Zero(t, *calls)
			require.Empty(t, again.Snapshot().Registry.Resolution.Bindings)
			state, err = again.GetEntry(state.ID)
			require.NoError(t, err)
			require.Equal(t, []any{"scope:default"}, state.Data.Data().(map[string]any)["groups"])
		})
	}
}

func TestRecordedBindingsFollowClearsOwnerReplacementAndDeploymentChange(t *testing.T) {
	for _, engine := range []string{"sqlite", "postgres"} {
		for _, action := range []string{"clear", "clear-no-artifact-parameters", "unrelated", "owner-replacement", "deployment-change", "deployment-artifact-defaults"} {
			t.Run(engine+"/"+action, func(t *testing.T) {
				fixture := newBindingRestartFixture(t)
				if action == "clear-no-artifact-parameters" {
					entries := bindingAppEntries(42)
					entries[2].Data = DependencyDefinition{Component: "acme/worker", Version: "2.0.0"}
					setBindingFixtureArtifact(t, fixture, "app@2.0.0", entries)
					workerEntries := bindingWorkerEntries()
					for i := range workerEntries {
						if workerEntries[i].Kind == regapi.NamespaceRequirement {
							defaults := map[string]any{"enabled": false, "limit": 7, "introduced": 42}
							if value, exists := defaults[workerEntries[i].ID.Name]; exists {
								workerEntries[i].Data.(map[string]any)["default"] = value
							}
						}
					}
					setBindingFixtureArtifact(t, fixture, "worker@2.0.0", workerEntries)
				}
				openHistory := deploymentRestartHistory(t, engine)
				history, closeHistory := openHistory()
				ctx, live, _ := fixture.registry(t, history, false)
				root, err := history.Head()
				require.NoError(t, err)
				require.NoError(t, live.LoadState(ctx, fixture.baseline, root))
				worker, err := live.GetEntry(regapi.NewID("app.deps", "worker"))
				require.NoError(t, err)
				worker.Data = payload.New(DependencyDefinition{Component: "acme/worker", Version: "2.0.0", Parameters: []Parameter{
					{Name: "scope", Value: "scope:overridden"}, {Name: "enabled", Value: true}, {Name: "limit", Value: 10},
				}})
				app := regapi.Entry{ID: regapi.NewID("app.deps", "application"), Kind: regapi.NamespaceDependency,
					Data: payload.New(DependencyDefinition{Component: "acme/app", Version: ">=2.0.0"})}
				head, err := live.Apply(ctx, regapi.ChangeSet{{Kind: regapi.EntryUpdate, Entry: worker}, {Kind: regapi.EntryCreate, Entry: app}})
				require.NoError(t, err)
				baseline := fixture.baseline
				wantScope, wantEnabled, wantLimit, wantIntroduced := "scope:overridden", true, 10, 42
				deploymentChange := action == "deployment-change" || action == "deployment-artifact-defaults"
				switch action {
				case "clear", "clear-no-artifact-parameters":
					worker.Data = payload.New(DependencyDefinition{Component: "acme/worker", Version: "2.0.0"})
					head, err = live.Apply(ctx, regapi.ChangeSet{{Kind: regapi.EntryUpdate, Entry: worker}})
					wantScope, wantEnabled, wantLimit = "scope:artifact", false, 7
					if action == "clear-no-artifact-parameters" {
						wantScope = "scope:default"
						found := false
						for _, binding := range live.Snapshot().Registry.Resolution.Bindings {
							if binding.ID == worker.ID.String() {
								found = true
								require.JSONEq(t, `[]`, string(binding.Parameters), "an empty binding remains an explicit modern decision")
							}
						}
						require.True(t, found)
					}
				case "owner-replacement":
					head, err = live.Apply(ctx, regapi.ChangeSet{{Kind: regapi.EntryUpdate, Entry: app}})
					wantScope, wantEnabled, wantLimit = "scope:artifact", false, 7
				case "unrelated":
					head, err = live.Apply(ctx, regapi.ChangeSet{{Kind: regapi.EntryCreate, Entry: regapi.Entry{
						ID: regapi.NewID("host.deps", "alias"), Kind: regapi.NamespaceDependency,
						Data: payload.New(DependencyDefinition{Component: "acme/worker", Version: "2.0.0"}),
					}}})
				case "deployment-change":
					baseline = append(append(regapi.State(nil), baseline...), regapi.Entry{
						ID: regapi.NewID("host.deps", "new-source"), Kind: regapi.NamespaceDependency,
						Registry: regapi.EntryMetadata{Root: true},
						Data:     payload.New(DependencyDefinition{Component: "acme/worker", Version: ">=2.0.0"}),
					})
				case "deployment-artifact-defaults":
					entries := bindingAppEntries(99)
					entries[1].Data = map[string]any{"generation": "3.0.0"}
					setBindingFixtureArtifact(t, fixture, "app@3.0.0", entries)
					baseline = nil
					for _, entry := range fixture.baseline {
						if entry.Registry.Owner != "acme/app" {
							baseline = append(baseline, entry)
						}
					}
					for _, entry := range entries {
						baseline = append(baseline, regapi.Entry{ID: regapi.NewID(entry.ID.Namespace, entry.ID.Name), Kind: entry.Kind,
							Data: payload.New(entry.Data), Registry: regapi.EntryMetadata{Owner: "acme/app", Root: entry.Kind == regapi.NamespaceDependency}})
					}
					l, loadErr := lock.New(fixture.lockPath)
					require.NoError(t, loadErr)
					l.SetModule(lock.Module{Name: "acme/app", Version: "3.0.0", Root: true, Hash: strings.TrimPrefix(fixture.digests["app@3.0.0"], "sha256:")})
					require.NoError(t, l.Write())
					require.NoError(t, os.WriteFile(filepath.Join(fixture.vendorDir, "acme", "app-3.0.0.wapp"), fixture.artifacts["app@3.0.0"], 0o600))
					wantIntroduced = 99
				}
				require.NoError(t, err)
				closeHistory()

				for restart := 0; restart < 2; restart++ {
					history, closeHistory = openHistory()
					offline := !deploymentChange || restart > 0
					ctx, restored, calls := fixture.registry(t, history, offline)
					require.NoError(t, restored.LoadState(ctx, baseline, head))
					if offline {
						require.Zero(t, *calls)
					}
					after, err := restored.GetEntry(regapi.NewID("acme.worker", "state"))
					require.NoError(t, err)
					require.Equal(t, []any{wantScope}, after.Data.Data().(map[string]any)["groups"])
					require.Equal(t, wantEnabled, after.Meta["enabled"])
					require.EqualValues(t, wantLimit, after.Meta["limit"])
					require.EqualValues(t, wantIntroduced, after.Meta["introduced"])
					closeHistory()
				}
			})
		}
	}
}

type bindingFailureRunner struct {
	bootRecordingRunner
	reject bool
}

func (r *bindingFailureRunner) Transition(ctx context.Context, state regapi.State, changes regapi.ChangeSet, abort func(context.Context)) (regapi.State, error) {
	if r.reject {
		return nil, errors.New("injected binding activation failure")
	}
	return r.bootRecordingRunner.Transition(ctx, state, changes, abort)
}

type bindingFailureHistory struct {
	regapi.ResolutionHeadCASHistory
	reject bool
}

func (h *bindingFailureHistory) SaveWithDependencyResolution(v regapi.Version, changes regapi.ChangeSet, resolution *regapi.DependencyResolution, moveHead bool) error {
	if h.reject {
		return errors.New("injected binding save failure")
	}
	return h.ResolutionHeadCASHistory.SaveWithDependencyResolution(v, changes, resolution, moveHead)
}

func TestRejectedBindingUpdateLeavesSnapshotAndHistoryUnchanged(t *testing.T) {
	for _, engine := range []string{"sqlite", "postgres"} {
		for _, failure := range []string{"resolution", "activation", "save"} {
			t.Run(engine+"/"+failure, func(t *testing.T) {
				fixture := newBindingRestartFixture(t)
				openHistory := deploymentRestartHistory(t, engine)
				history, closeHistory := openHistory()
				runner := &bindingFailureRunner{}
				durable := &bindingFailureHistory{ResolutionHeadCASHistory: history.(regapi.ResolutionHeadCASHistory)}
				ctx, live, _ := fixture.registryWithRunner(t, durable, false, false, runner)
				root, err := history.Head()
				require.NoError(t, err)
				require.NoError(t, live.LoadState(ctx, fixture.baseline, root))
				worker, err := live.GetEntry(regapi.NewID("app.deps", "worker"))
				require.NoError(t, err)
				worker.Data = payload.New(DependencyDefinition{Component: "acme/worker", Version: "2.0.0", Parameters: []Parameter{{Name: "scope", Value: "scope:before"}}})
				head, err := live.Apply(ctx, regapi.ChangeSet{
					{Kind: regapi.EntryUpdate, Entry: worker},
					{Kind: regapi.EntryCreate, Entry: regapi.Entry{ID: regapi.NewID("app.deps", "application"), Kind: regapi.NamespaceDependency,
						Data: payload.New(DependencyDefinition{Component: "acme/app", Version: ">=2.0.0"})}},
				})
				require.NoError(t, err)
				before := live.Snapshot().Registry.Resolution.Canonical()
				state, err := live.GetEntry(regapi.NewID("acme.worker", "state"))
				require.NoError(t, err)
				wantState := bindingEntryJSON(t, state)
				definition := DependencyDefinition{Component: "acme/worker", Version: "2.0.0", Parameters: []Parameter{{Name: "scope", Value: "scope:after"}}}
				switch failure {
				case "resolution":
					definition.Version = ">=9.0.0"
				case "activation":
					runner.reject = true
				case "save":
					durable.reject = true
				}
				worker.Data = payload.New(definition)
				_, err = live.Apply(ctx, regapi.ChangeSet{{Kind: regapi.EntryUpdate, Entry: worker}})
				require.Error(t, err)
				require.Equal(t, before, live.Snapshot().Registry.Resolution)
				state, err = live.GetEntry(state.ID)
				require.NoError(t, err)
				require.Equal(t, wantState, bindingEntryJSON(t, state))
				stored, err := durable.GetDependencyResolution(head)
				require.NoError(t, err)
				require.Equal(t, before, stored)
				current, err := history.Head()
				require.NoError(t, err)
				require.Equal(t, head.ID(), current.ID())
				// A rejected operation must not leave hidden solver state that
				// prevents a valid retry on the same running registry.
				runner.reject = false
				durable.reject = false
				definition.Version = "2.0.0"
				worker.Data = payload.New(definition)
				retried, err := live.Apply(ctx, regapi.ChangeSet{{Kind: regapi.EntryUpdate, Entry: worker}})
				require.NoError(t, err)
				require.NotEqual(t, head.ID(), retried.ID())
				afterRetry := live.Snapshot().Registry.Resolution.Canonical()
				state, err = live.GetEntry(state.ID)
				require.NoError(t, err)
				require.Equal(t, []any{"scope:after"}, state.Data.Data().(map[string]any)["groups"])
				wantRetryState := bindingEntryJSON(t, state)
				closeHistory()
				history, closeHistory = openHistory()
				defer closeHistory()
				ctx, restored, calls := fixture.registry(t, history, true)
				require.NoError(t, restored.LoadState(ctx, fixture.baseline, retried))
				require.Zero(t, *calls)
				require.Equal(t, afterRetry, restored.Snapshot().Registry.Resolution)
				state, err = restored.GetEntry(state.ID)
				require.NoError(t, err)
				require.Equal(t, wantRetryState, bindingEntryJSON(t, state))
				require.NoError(t, restored.ApplyVersion(ctx, head))
				require.Equal(t, before, restored.Snapshot().Registry.Resolution)
				state, err = restored.GetEntry(state.ID)
				require.NoError(t, err)
				require.Equal(t, wantState, bindingEntryJSON(t, state))
				require.NoError(t, restored.ApplyVersion(ctx, retried))
				require.Equal(t, afterRetry, restored.Snapshot().Registry.Resolution)
				state, err = restored.GetEntry(state.ID)
				require.NoError(t, err)
				require.Equal(t, wantRetryState, bindingEntryJSON(t, state))
				require.Zero(t, *calls, "restart and undo/redo must remain offline")
			})
		}
	}
}

func bindingEntryJSON(t *testing.T, entry regapi.Entry) string {
	t.Helper()
	encoded, err := json.Marshal(struct {
		Meta any `json:"meta"`
		Data any `json:"data"`
	}{entry.Meta, entry.Data.Data()})
	require.NoError(t, err)
	return string(encoded)
}

func newBindingRestartFixture(t *testing.T) *deploymentRestartFixture {
	t.Helper()
	f := newDeploymentRestartFixture(t)
	for key, entries := range map[string][]wapp.Entry{"worker@2.0.0": bindingWorkerEntries(), "app@2.0.0": bindingAppEntries(42)} {
		setBindingFixtureArtifact(t, f, key, entries)
	}
	return f
}

func bindingWorkerEntries() []wapp.Entry {
	worker := []wapp.Entry{
		{ID: wapp.NewID("acme.worker", "definition"), Kind: regapi.NamespaceDefinition},
		{ID: wapp.NewID("acme.worker", "state"), Kind: regapi.EntryKind, Meta: map[string]any{}, Data: map[string]any{"generation": "2.0.0", "groups": []any{}}},
		{ID: wapp.NewID("acme.worker", "scope"), Kind: regapi.NamespaceRequirement,
			Data: map[string]any{"default": "scope:default", "targets": []any{map[string]any{"entry": "state", "path": ".groups +="}}}},
	}
	for _, name := range []string{"enabled", "limit", "introduced"} {
		worker = append(worker, wapp.Entry{ID: wapp.NewID("acme.worker", name), Kind: regapi.NamespaceRequirement,
			Data: map[string]any{"targets": []any{map[string]any{"entry": "state", "path": ".meta." + name}}}})
	}
	return worker
}

func bindingAppEntries(introduced int) []wapp.Entry {
	return []wapp.Entry{
		{ID: wapp.NewID("acme.app", "definition"), Kind: regapi.NamespaceDefinition},
		{ID: wapp.NewID("acme.app", "state"), Kind: regapi.EntryKind, Data: map[string]any{"generation": "2.0.0"}},
		{ID: wapp.NewID("app.deps", "worker"), Kind: regapi.NamespaceDependency,
			Data: DependencyDefinition{Component: "acme/worker", Version: "2.0.0", Parameters: []Parameter{
				{Name: "scope", Value: "scope:artifact"}, {Name: "enabled", Value: false}, {Name: "limit", Value: 7}, {Name: "introduced", Value: introduced},
			}}},
	}
}

func setBindingFixtureArtifact(t *testing.T, fixture *deploymentRestartFixture, key string, entries []wapp.Entry) {
	t.Helper()
	fixture.artifacts[key] = buildWappBytes(t, entries)
	sum := sha256.Sum256(fixture.artifacts[key])
	fixture.digests[key] = "sha256:" + hex.EncodeToString(sum[:])
}
