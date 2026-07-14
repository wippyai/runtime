// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/internal/version"
	registryimpl "github.com/wippyai/runtime/system/registry"
	historymem "github.com/wippyai/runtime/system/registry/history/memory"
	"github.com/wippyai/runtime/system/registry/topology"
	"github.com/wippyai/wapp"
	"go.uber.org/zap"
)

type bootRecordingRunner struct {
	transitions []regapi.ChangeSet
}

type bootDirectiveFunc func(context.Context, regapi.Operation, regapi.State) (regapi.DirectiveResult, error)

func (f bootDirectiveFunc) Expand(ctx context.Context, op regapi.Operation, state regapi.State) (regapi.DirectiveResult, error) {
	return f(ctx, op, state)
}

func (r *bootRecordingRunner) Transition(_ context.Context, state regapi.State, changes regapi.ChangeSet) (regapi.State, error) {
	r.transitions = append(r.transitions, append(regapi.ChangeSet(nil), changes...))
	stateMap := topology.NewStateMap(state)
	for _, op := range changes {
		switch op.Kind {
		case regapi.EntryCreate, regapi.EntryUpdate:
			stateMap[op.Entry.ID] = op.Entry
		case regapi.EntryDelete:
			delete(stateMap, op.Entry.ID)
		}
	}
	return topology.StateMapToSlice(stateMap), nil
}

func TestDependencyHandler_BootExpandsSourceRootBeforeUnrelatedInstall(t *testing.T) {
	ctx := newTestContext()
	tmpDir := t.TempDir()
	vendorDir := filepath.Join(tmpDir, "vendor")

	artifacts := map[string][]byte{
		"analysis": buildWappBytes(t, []wapp.Entry{{
			ID:   wapp.NewID("acme.analysis", "runtime"),
			Kind: "process.host",
			Data: map[string]any{"runtime": "wasm"},
		}}),
		"crm": buildWappBytes(t, []wapp.Entry{{
			ID:   wapp.NewID("acme.crm", "service"),
			Kind: "service",
			Data: map[string]any{"enabled": true},
		}}),
	}
	downloads := map[string]int{}

	hubClient := &fakeHub{
		getManifest: func(_ context.Context, org, module, _ string) (*ModuleManifest, error) {
			if _, ok := artifacts[module]; !ok {
				return nil, fmt.Errorf("unknown module %s/%s", org, module)
			}
			return &ModuleManifest{
				Org: org, Name: module, Version: "v1.0.0", URL: "memory://" + module,
			}, nil
		},
		downloadFile: func(_ context.Context, url, destPath string) error {
			module := url[len("memory://"):]
			artifact, ok := artifacts[module]
			if !ok {
				return fmt.Errorf("unknown artifact %q", url)
			}
			downloads[module]++
			if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
				return err
			}
			return os.WriteFile(destPath, artifact, 0o600)
		},
	}

	resolver := topology.NewResolver()
	handler, err := NewDependencyHandler(DependencyHandlerOptions{
		Hub:       hubClient,
		Logger:    zap.NewNop(),
		Resolver:  resolver,
		LockPath:  filepath.Join(tmpDir, "wippy.lock"),
		VendorDir: vendorDir,
	})
	require.NoError(t, err)

	runner := &bootRecordingRunner{}
	reg := registryimpl.NewRegistry(
		historymem.New(),
		runner,
		topology.NewStateBuilder(zap.NewNop(), resolver),
		resolver,
		zap.NewNop(),
		registryimpl.WithKindDirective(regapi.NamespaceDependency, bootDirectiveFunc(handler.Expand)),
	)

	analysisRoot := regapi.Entry{
		ID:   regapi.NewID("app.deps", "analysis"),
		Kind: regapi.NamespaceDependency,
		Data: payload.NewPayload(`{"component":"acme/analysis","version":"v1.0.0"}`, payload.JSON),
	}
	require.NoError(t, reg.LoadState(ctx, regapi.State{analysisRoot}, version.FromParent(nil, 0)))

	_, err = reg.GetEntry(regapi.NewID("acme.analysis", "runtime"))
	require.NoError(t, err, "boot must not publish a dependency root without its module entries")
	require.Equal(t, 1, downloads["analysis"], "the declared module must be installed during boot")

	runner.transitions = nil
	crmRoot := regapi.Entry{
		ID:   regapi.NewID("app.deps", "crm"),
		Kind: regapi.NamespaceDependency,
		Data: payload.NewPayload(`{"component":"acme/crm","version":"v1.0.0"}`, payload.JSON),
	}
	_, err = reg.Apply(ctx, regapi.ChangeSet{{Kind: regapi.EntryCreate, Entry: crmRoot}})
	require.NoError(t, err)
	require.Equal(t, 1, downloads["analysis"], "installing CRM must not materialize or reinstall Analysis")
	require.Equal(t, 1, downloads["crm"])

	for _, transition := range runner.transitions {
		for _, op := range transition {
			require.NotEqual(t, regapi.NewID("acme.analysis", "runtime"), op.Entry.ID,
				"an unrelated install must not mutate the module established at boot")
		}
	}
}
