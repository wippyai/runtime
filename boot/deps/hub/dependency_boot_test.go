// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/internal/version"
	registryimpl "github.com/wippyai/runtime/system/registry"
	regexp "github.com/wippyai/runtime/system/registry/expansion"
	historymem "github.com/wippyai/runtime/system/registry/history/memory"
	historysqlite "github.com/wippyai/runtime/system/registry/history/sqlite"
	"github.com/wippyai/runtime/system/registry/topology"
	"github.com/wippyai/wapp"
	"go.uber.org/zap"
)

type bootRecordingRunner struct {
	transitions []regapi.ChangeSet
}

func TestDependencyHandler_PersistedResolutionBootRollbackRedoAndLongHistory(t *testing.T) {
	ctx := newTestContext()
	vendorDir := filepath.Join(t.TempDir(), "vendor")
	artifacts := map[string][]byte{
		"v1.0.0": buildWappBytes(t, []wapp.Entry{{
			ID: wapp.NewID("acme.crm", "service"), Kind: "service", Data: map[string]any{"version": "v1"},
		}}),
		"v2.0.0": buildWappBytes(t, []wapp.Entry{{
			ID: wapp.NewID("acme.crm", "service"), Kind: "service", Data: map[string]any{"version": "v2"},
		}}),
	}
	downloads := 0
	online := &fakeHub{
		getManifest: func(_ context.Context, org, module, constraint string) (*ModuleManifest, error) {
			artifact, ok := artifacts[constraint]
			if !ok {
				return nil, fmt.Errorf("unexpected constraint %q", constraint)
			}
			sum := sha256.Sum256(artifact)
			return &ModuleManifest{
				Org: org, Name: module, Version: constraint, VersionID: constraint,
				Digest: hex.EncodeToString(sum[:]), SizeBytes: uint64(len(artifact)), URL: "memory://" + constraint,
			}, nil
		},
		downloadFile: func(_ context.Context, url, destPath string) error {
			version := strings.TrimPrefix(url, "memory://")
			downloads++
			if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
				return err
			}
			return os.WriteFile(destPath, artifacts[version], 0o600)
		},
	}
	newHandler := func(client HubClient) *DependencyHandler {
		handler, err := NewDependencyHandler(DependencyHandlerOptions{
			Hub: client, Logger: zap.NewNop(), LockPath: filepath.Join(filepath.Dir(vendorDir), "wippy.lock"), VendorDir: vendorDir,
		})
		require.NoError(t, err)
		return handler
	}
	newRegistry := func(hist regapi.History, handler *DependencyHandler) *registryimpl.Reg {
		resolver := topology.NewResolver()
		handler.resolver = resolver
		return registryimpl.NewRegistry(
			hist, &bootRecordingRunner{}, topology.NewStateBuilder(zap.NewNop(), resolver), resolver, zap.NewNop(),
			registryimpl.WithKindDirective(regapi.NamespaceDependency, regexp.NewDependencyDirective(handler.Expand).WithResolutionTransition(handler.ReconcileResolution)),
		)
	}

	dbPath := filepath.Join(t.TempDir(), "registry.db")
	history, err := historysqlite.NewSQLite(dbPath, zap.NewNop())
	require.NoError(t, err)
	reg := newRegistry(history, newHandler(online))
	v1Root := regapi.Entry{
		ID: regapi.NewID("app.deps", "crm"), Kind: regapi.NamespaceDependency,
		Data: payload.NewPayload(`{"component":"acme/crm","version":"v1.0.0"}`, payload.JSON),
	}
	v1, err := reg.Apply(ctx, regapi.ChangeSet{{Kind: regapi.EntryCreate, Entry: v1Root}})
	require.NoError(t, err)
	require.Equal(t, 1, downloads)
	v1Resolution, err := history.GetDependencyResolution(v1)
	require.NoError(t, err)
	require.Equal(t, "v1.0.0", v1Resolution.Modules[0].Version)

	// A long tail of unrelated versions inherits one content-addressed graph.
	for i := 0; i < 250; i++ {
		_, err = reg.Apply(ctx, regapi.ChangeSet{{
			Kind:  regapi.EntryCreate,
			Entry: regapi.Entry{ID: regapi.NewID("app.settings", fmt.Sprintf("entry_%03d", i)), Kind: regapi.EntryKind, Data: payload.New(i)},
		}})
		require.NoError(t, err)
	}

	v2Root := v1Root
	v2Root.Data = payload.NewPayload(`{"component":"acme/crm","version":"v2.0.0"}`, payload.JSON)
	_, err = reg.Apply(ctx, regapi.ChangeSet{{Kind: regapi.EntryUpdate, Entry: v2Root}})
	require.NoError(t, err)
	require.Equal(t, 2, downloads)
	require.NoError(t, history.Close())

	// Reopen the actual history database so the restore cannot accidentally
	// depend on in-memory registry or resolution state.
	history, err = historysqlite.NewSQLite(dbPath, zap.NewNop())
	require.NoError(t, err)
	t.Cleanup(func() { _ = history.Close() })
	v2, err := history.Head()
	require.NoError(t, err)
	versions, err := history.Versions()
	require.NoError(t, err)
	require.Greater(t, len(versions), 1)
	v1 = versions[1]

	manifestCalls := 0
	offline := &fakeHub{
		getManifest: func(context.Context, string, string, string) (*ModuleManifest, error) {
			manifestCalls++
			return nil, errors.New("resolver must not run while restoring an exact graph")
		},
		getDownload: func(context.Context, *DownloadParams) (*DownloadInfo, error) {
			return nil, errors.New("cached exact artifacts should be used")
		},
	}
	restored := newRegistry(history, newHandler(offline))
	require.NoError(t, restored.LoadState(ctx, nil, v2))
	require.Zero(t, manifestCalls)
	entry, err := restored.GetEntry(regapi.NewID("acme.crm", "service"))
	require.NoError(t, err)
	require.Equal(t, "v2.0.0", moduleVersion(entry))

	// Undo and redo select stored graphs and never resolve them again.
	require.NoError(t, restored.ApplyVersion(ctx, v1))
	entry, err = restored.GetEntry(regapi.NewID("acme.crm", "service"))
	require.NoError(t, err)
	require.Equal(t, "v1.0.0", moduleVersion(entry))
	require.NoError(t, restored.ApplyVersion(ctx, v2))
	entry, err = restored.GetEntry(regapi.NewID("acme.crm", "service"))
	require.NoError(t, err)
	require.Equal(t, "v2.0.0", moduleVersion(entry))
	require.Zero(t, manifestCalls)
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
