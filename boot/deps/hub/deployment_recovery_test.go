// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/boot/deps/graph"
	"github.com/wippyai/runtime/boot/deps/lock"
	"go.uber.org/zap"
)

func TestDeploymentRestartWithoutLock(t *testing.T) {
	for _, engine := range []string{"sqlite", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			fixture := newDeploymentRestartFixture(t)
			history, closeHistory := deploymentRestartHistory(t, engine)()
			defer closeHistory()
			ctx, reg, _ := fixture.registry(t, history, false)
			root, err := history.Head()
			require.NoError(t, err)
			require.NoError(t, reg.LoadState(ctx, fixture.baseline, root))
			head := fixture.update(ctx, t, reg)
			selected := reg.Snapshot().Registry.Resolution
			require.NoError(t, os.Rename(fixture.lockPath, fixture.lockPath+".saved"))
			fixture.lockPath = ""
			ctx, reg, calls := fixture.registry(t, history, true)
			require.NoError(t, reg.LoadState(ctx, fixture.baseline, head))
			require.Zero(t, *calls)
			require.Equal(t, selected, reg.Snapshot().Registry.Resolution,
				"standalone boot uses its persisted deployment, not the updated graph as its baseline")
		})
	}
}

func TestDeploymentSourceCheckoutRestart(t *testing.T) {
	for _, engine := range []string{"sqlite", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			fixture := newDeploymentRestartFixture(t)
			l, err := lock.New(fixture.lockPath)
			require.NoError(t, err)
			l.SetRootModule("")
			require.NoError(t, l.Write())
			for i := range fixture.baseline {
				if fixture.baseline[i].Kind == regapi.NamespaceDependency {
					fixture.baseline[i].Registry.Owner = "" // host src declaration
				}
			}
			history, closeHistory := deploymentRestartHistory(t, engine)()
			defer closeHistory()
			ctx, reg, _ := fixture.registryForModel(t, history, false, true)
			root, err := history.Head()
			require.NoError(t, err)
			require.NoError(t, reg.LoadState(ctx, fixture.baseline, root))
			worker, err := reg.GetEntry(regapi.NewID("app.deps", "worker"))
			require.NoError(t, err)
			worker.Data = payload.New(map[string]any{"component": "acme/worker", "version": ">=2.0.0"})
			head, err := reg.Apply(ctx, regapi.ChangeSet{{Kind: regapi.EntryUpdate, Entry: worker}})
			require.NoError(t, err)
			selected := reg.Snapshot().Registry.Resolution
			ctx, reg, calls := fixture.registry(t, history, true)
			require.NoError(t, reg.LoadState(ctx, fixture.baseline, head))
			require.Zero(t, *calls)
			restored := reg.Snapshot().Registry.Resolution
			require.Nil(t, restored.Deployment, "a source checkout must not become a rooted Hub deployment")
			require.Equal(t, selected.Modules, restored.Modules)
			require.Equal(t, selected.Roots, restored.Roots)
		})
	}
}

func TestDeploymentLegacyRawLockDigestRecovery(t *testing.T) {
	for _, engine := range []string{"sqlite", "postgres"} {
		t.Run(engine, func(t *testing.T) {
			fixture := newDeploymentRestartFixture(t)
			l, err := lock.New(fixture.lockPath)
			require.NoError(t, err)
			for _, module := range l.GetModules() {
				module.Hash = "sha256:" + strings.ToUpper(module.Hash)
				l.SetModule(module)
			}
			require.NoError(t, l.Write())
			history, closeHistory := deploymentRestartHistory(t, engine)()
			defer closeHistory()
			ctx, reg, _ := fixture.registryForModel(t, history, false, true)
			root, err := history.Head()
			require.NoError(t, err)
			require.NoError(t, reg.LoadState(ctx, fixture.baseline, root))
			head := fixture.update(ctx, t, reg)
			selected := reg.Snapshot().Registry.Resolution
			ctx, reg, calls := fixture.registry(t, history, true)
			require.NoError(t, reg.LoadState(ctx, fixture.baseline, head))
			require.Zero(t, *calls)
			require.Equal(t, selected.Modules, reg.Snapshot().Registry.Resolution.Modules)
		})
	}
}

type baselineActivationFailure struct{}

func (baselineActivationFailure) Transition(context.Context, regapi.State, regapi.ChangeSet, func(context.Context)) (regapi.State, error) {
	return nil, errors.New("injected baseline activation failure")
}

type baselineContextRunner struct {
	t *testing.T
	bootRecordingRunner
}

func (r *baselineContextRunner) Transition(ctx context.Context, state regapi.State, changes regapi.ChangeSet, abort func(context.Context)) (regapi.State, error) {
	_, present := regapi.DependencyBaselineFromContext(ctx)
	require.False(r.t, present, "runners and long-lived actors must not retain directive replay context")
	return r.bootRecordingRunner.Transition(ctx, state, changes, abort)
}

func TestDeploymentBaselineContextIsDirectiveScoped(t *testing.T) {
	fixture := newDeploymentRestartFixture(t)
	history, closeHistory := deploymentRestartHistory(t, "sqlite")()
	defer closeHistory()
	ctx, reg, _ := fixture.registryWithRunner(t, history, false, false, &baselineContextRunner{t: t})
	root, err := history.Head()
	require.NoError(t, err)
	require.NoError(t, reg.LoadState(ctx, fixture.baseline, root))
	head := fixture.update(ctx, t, reg)
	require.NoError(t, reg.ApplyVersion(ctx, root))
	require.NoError(t, reg.ApplyVersion(ctx, head))
	ctx, reg, _ = fixture.registryWithRunner(t, history, true, false, &baselineContextRunner{t: t})
	require.NoError(t, reg.LoadState(ctx, fixture.baseline, head))
}

type baselineCheckpointFailure struct {
	regapi.ResolutionHeadCASHistory
}

func (baselineCheckpointFailure) CompareAndSetHeadWithDependencyResolution(regapi.Version, regapi.Version, *regapi.DependencyResolution) error {
	return errors.New("injected baseline checkpoint failure")
}

func TestDeploymentRecoveryFailureDoesNotRestampHistory(t *testing.T) {
	for _, engine := range []string{"sqlite", "postgres"} {
		for _, failure := range []string{"activation", "checkpoint", "missing-artifact", "corrupt-artifact"} {
			t.Run(engine+"/"+failure, func(t *testing.T) {
				fixture := newDeploymentRestartFixture(t)
				history, closeHistory := deploymentRestartHistory(t, engine)()
				defer closeHistory()
				ctx, reg, _ := fixture.registryForModel(t, history, false, true)
				root, err := history.Head()
				require.NoError(t, err)
				require.NoError(t, reg.LoadState(ctx, fixture.baseline, root))
				head := fixture.update(ctx, t, reg)
				before, err := history.(regapi.ResolutionHistory).GetDependencyResolution(head)
				require.NoError(t, err)
				switch failure {
				case "activation":
					ctx, reg, _ = fixture.registryWithRunner(t, history, true, false, baselineActivationFailure{})
					err = reg.LoadState(ctx, fixture.baseline, head)
				case "checkpoint":
					failing := baselineCheckpointFailure{history.(regapi.ResolutionHeadCASHistory)}
					ctx, reg, _ = fixture.registry(t, failing, true)
					err = reg.LoadState(ctx, fixture.baseline, head)
				default:
					handler, createErr := NewDependencyHandler(DependencyHandlerOptions{
						Hub: &fakeHub{}, Logger: zap.NewNop(), LockPath: fixture.lockPath, VendorDir: fixture.vendorDir,
					})
					require.NoError(t, createErr)
					name, parseErr := graph.ParseName("acme/worker")
					require.NoError(t, parseErr)
					path, pathErr := handler.immutableArtifactPath(name, "2.0.0", fixture.digests["worker@2.0.0"])
					require.NoError(t, pathErr)
					if failure == "missing-artifact" {
						require.NoError(t, os.Remove(path))
					} else {
						require.NoError(t, os.WriteFile(path, []byte("corrupt artifact"), 0o600))
					}
					err = handler.PrepareRestore(regapi.WithDependencyAccess(newTestContext(), regapi.DependencyAccessVerifiedOffline), history)
					require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
					require.NoError(t, os.WriteFile(path, fixture.artifacts["worker@2.0.0"], 0o600))
				}
				require.Error(t, err)
				after, getErr := history.(regapi.ResolutionHistory).GetDependencyResolution(head)
				require.NoError(t, getErr)
				require.Equal(t, before, after, "failed recovery must leave the old checkpoint intact")
				current, headErr := history.Head()
				require.NoError(t, headErr)
				require.Equal(t, head.ID(), current.ID())
				ctx, reg, calls := fixture.registry(t, history, true)
				require.NoError(t, reg.LoadState(ctx, fixture.baseline, head), "recovery can be retried safely")
				require.Zero(t, *calls)
				require.Equal(t, before.Modules, reg.Snapshot().Registry.Resolution.Modules)
			})
		}
	}
}
