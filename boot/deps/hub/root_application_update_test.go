// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/internal/version"
	registryimpl "github.com/wippyai/runtime/system/registry"
	regexp "github.com/wippyai/runtime/system/registry/expansion"
	historymem "github.com/wippyai/runtime/system/registry/history/memory"
	"github.com/wippyai/runtime/system/registry/topology"
	"github.com/wippyai/wapp"
	"go.uber.org/zap"
)

func TestDependencyHandler_RootApplicationUpdatePreservesNestedDependencyRoots(t *testing.T) {
	ctx := newTestContext()
	tmpDir := t.TempDir()
	lockPath := filepath.Join(tmpDir, "wippy.lock")
	vendorDir := filepath.Join(tmpDir, "vendor")

	type selection struct{ module, version string }
	// Root provenance is selected by the lock graph, not by an app.deps naming
	// convention. Use deliberately arbitrary managed namespaces in this proof.
	workerRootID := regapi.NewID("workspace.modules", "worker")
	adminPolicyID := regapi.NewID("deployment.security", "admin")
	artifacts := map[selection][]byte{
		{"acme/app", "v1.0.0"}: buildWappBytes(t, []wapp.Entry{
			{ID: wapp.NewID("acme.app", "service"), Kind: "service", Data: map[string]any{"version": "v1"}},
			{ID: wapp.NewID(adminPolicyID.NS, adminPolicyID.Name), Kind: "security.policy", Data: map[string]any{"allow": true}},
			{ID: wapp.NewID(workerRootID.NS, workerRootID.Name), Kind: regapi.NamespaceDependency,
				Data: map[string]any{
					"component": "acme/worker", "version": "v1.0.0",
					"parameters": []any{map[string]any{"name": "acme.worker:router", "value": "app:api"}},
				}},
		}),
		{"acme/app", "v2.0.0"}: buildWappBytes(t, []wapp.Entry{
			{ID: wapp.NewID("acme.app", "service"), Kind: "service", Data: map[string]any{"version": "v2"}},
			{ID: wapp.NewID(workerRootID.NS, workerRootID.Name), Kind: regapi.NamespaceDependency,
				Data: map[string]any{
					"component": "acme/worker", "version": "v1.0.0",
					"parameters": []any{map[string]any{"name": "acme.worker:router", "value": "app:api"}},
				}},
		}),
		{"acme/worker", "v1.0.0"}: buildWappBytes(t, []wapp.Entry{{
			ID: wapp.NewID("acme.worker", "service"), Kind: "service",
		}}),
	}
	digests := make(map[selection]string, len(artifacts))
	for selected, artifact := range artifacts {
		sum := sha256.Sum256(artifact)
		digests[selected] = "sha256:" + hex.EncodeToString(sum[:])
	}
	require.NoError(t, os.MkdirAll(filepath.Join(vendorDir, "acme"), 0o755))
	for _, selected := range []selection{{"acme/app", "v1.0.0"}, {"acme/worker", "v1.0.0"}} {
		require.NoError(t, os.WriteFile(
			filepath.Join(vendorDir, "acme", selected.module[len("acme/"):]+"-"+selected.version+".wapp"),
			artifacts[selected],
			0o600,
		))
	}
	require.NoError(t, os.WriteFile(lockPath, []byte(fmt.Sprintf(`directories:
  modules: vendor
modules:
  - name: acme/app
    version: v1.0.0
    hash: %s
    root: true
  - name: acme/worker
    version: v1.0.0
    hash: %s
`, digests[selection{"acme/app", "v1.0.0"}], digests[selection{"acme/worker", "v1.0.0"}])), 0o600))

	hubClient := &fakeHub{
		getManifest: func(_ context.Context, org, module, constraint string) (*ModuleManifest, error) {
			selected := selection{module: org + "/" + module, version: constraint}
			artifact, ok := artifacts[selected]
			if !ok {
				return nil, fmt.Errorf("unexpected manifest %s@%s", selected.module, selected.version)
			}
			manifest := &ModuleManifest{
				Org: org, Name: module, Version: constraint, VersionID: constraint,
				Digest: digests[selected], SizeBytes: uint64(len(artifact)),
				URL: "memory://" + module + "@" + constraint,
			}
			if selected.module == "acme/app" {
				manifest.Dependencies = []ManifestDep{{
					Org: "acme", Name: "worker", Version: "v1.0.0", Constraint: "v1.0.0",
					Digest: digests[selection{"acme/worker", "v1.0.0"}],
				}}
			}
			return manifest, nil
		},
		downloadFile: func(_ context.Context, url, destination string) error {
			var selected selection
			switch url {
			case "memory://app@v2.0.0":
				selected = selection{"acme/app", "v2.0.0"}
			case "memory://worker@v1.0.0":
				selected = selection{"acme/worker", "v1.0.0"}
			default:
				return fmt.Errorf("unexpected download %s", url)
			}
			if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
				return err
			}
			return os.WriteFile(destination, artifacts[selected], 0o600)
		},
	}
	handler, err := NewDependencyHandler(DependencyHandlerOptions{
		Hub: hubClient, Logger: zap.NewNop(), Resolver: topology.NewResolver(),
		LockPath: lockPath, VendorDir: vendorDir,
	})
	require.NoError(t, err)

	appRoot := regapi.Entry{
		ID: regapi.NewID("deployment.packages", "app"), Kind: regapi.NamespaceDependency, DependencyRoot: true,
		Data: payload.New(map[string]any{"component": "acme/app", "version": "v1.0.0"}),
	}
	workerRoot := markModuleIdentity(regapi.Entry{
		ID: workerRootID, Kind: regapi.NamespaceDependency, DependencyRoot: true,
		Data: payload.New(map[string]any{
			"component": "acme/worker", "version": "v1.0.0",
			"parameters": []any{map[string]any{"name": "acme.worker:router", "value": "app:api"}},
		}),
	}, "acme/app", "v1.0.0", "sha256:old-app")
	snapshot := regapi.State{
		appRoot,
		workerRoot,
		markModuleIdentity(regapi.Entry{ID: regapi.NewID("acme.app", "service"), Kind: "service"},
			"acme/app", "v1.0.0", "sha256:old-app"),
		markModuleIdentity(regapi.Entry{ID: regapi.NewID("acme.worker", "service"), Kind: "service"},
			"acme/worker", "v1.0.0", digests[selection{"acme/worker", "v1.0.0"}]),
	}
	updatedRoot := appRoot
	updatedRoot.Data = payload.New(map[string]any{"component": "acme/app", "version": "v2.0.0"})
	result, err := handler.Expand(ctx, regapi.Operation{Kind: regapi.EntryUpdate, Entry: updatedRoot}, snapshot)
	require.NoError(t, err)

	var nestedUpdate *regapi.Operation
	for i := range result.Additional {
		if result.Additional[i].Operation.Entry.ID == workerRootID {
			op := result.Additional[i].Operation
			nestedUpdate = &op
			break
		}
	}
	require.NotNil(t, nestedUpdate, "root package update must replace its nested dependency declaration")
	require.True(t, nestedUpdate.Entry.DependencyRoot,
		"dependencies declared by the selected root application must remain deployment roots")

	// Exercise the real root self-update shape. The deployment root is selected
	// by wippy.lock and therefore has no synthetic registry dependency at v0.
	// A live update creates an overlay dependency for the same component; undo
	// must reveal the locked package and all of its entries again.
	history := historymem.New()
	runner := &bootRecordingRunner{}
	reg := registryimpl.NewRegistry(
		history,
		runner,
		topology.NewStateBuilder(zap.NewNop(), handler.resolver),
		handler.resolver,
		zap.NewNop(),
		registryimpl.WithKindDirective(
			regapi.NamespaceDependency,
			regexp.NewDependencyDirective(handler.Expand).WithResolutionTransition(handler.ReconcileResolution),
		),
	)
	v1AppEntries, err := loadEntriesFromWappBytesForTest(artifacts[selection{"acme/app", "v1.0.0"}])
	require.NoError(t, err)
	for i := range v1AppEntries {
		v1AppEntries[i] = markModuleIdentity(v1AppEntries[i], "acme/app", "v1.0.0", digests[selection{"acme/app", "v1.0.0"}])
		if v1AppEntries[i].Kind == regapi.NamespaceDependency {
			v1AppEntries[i].DependencyRoot = true
		}
	}
	workerEntries, err := loadEntriesFromWappBytesForTest(artifacts[selection{"acme/worker", "v1.0.0"}])
	require.NoError(t, err)
	for i := range workerEntries {
		workerEntries[i] = markModuleIdentity(workerEntries[i], "acme/worker", "v1.0.0", digests[selection{"acme/worker", "v1.0.0"}])
	}
	baseline := append(v1AppEntries, workerEntries...)
	v0 := version.FromParent(nil, regapi.RootVersion)
	require.NoError(t, reg.LoadState(ctx, baseline, v0))
	v0Resolution, err := history.GetDependencyResolution(v0)
	require.NoError(t, err)
	require.Equal(t, "v1.0.0", resolutionModuleVersion(v0Resolution, "acme/app"),
		"the durable baseline graph must include its lock-selected root module")

	overlayRoot := regapi.Entry{
		ID: regapi.NewID("deployment.packages", "application"), Kind: regapi.NamespaceDependency,
		Data: payload.New(map[string]any{"component": "acme/app", "version": "v2.0.0"}),
	}
	v1, err := reg.Apply(ctx, regapi.ChangeSet{{Kind: regapi.EntryCreate, Entry: overlayRoot}})
	require.NoError(t, err)
	service, err := reg.GetEntry(regapi.NewID("acme.app", "service"))
	require.NoError(t, err)
	require.Equal(t, "v2.0.0", moduleVersion(service))
	_, err = reg.GetEntry(adminPolicyID)
	require.Error(t, err, "v2 fixture deliberately removes the v1 policy")

	require.NoError(t, reg.ApplyVersion(ctx, v0))
	service, err = reg.GetEntry(regapi.NewID("acme.app", "service"))
	require.NoError(t, err)
	require.Equal(t, "v1.0.0", moduleVersion(service))
	_, err = reg.GetEntry(adminPolicyID)
	require.NoError(t, err, "undo must reveal every entry from the locked root package")
	_, err = reg.GetEntry(overlayRoot.ID)
	require.Error(t, err)

	require.NoError(t, reg.ApplyVersion(ctx, v1))
	service, err = reg.GetEntry(regapi.NewID("acme.app", "service"))
	require.NoError(t, err)
	require.Equal(t, "v2.0.0", moduleVersion(service))
	_, err = reg.GetEntry(adminPolicyID)
	require.Error(t, err)
}

func loadEntriesFromWappBytesForTest(data []byte) ([]regapi.Entry, error) {
	file, err := os.CreateTemp("", "root-application-*.wapp")
	if err != nil {
		return nil, err
	}
	path := file.Name()
	defer os.Remove(path)
	if _, err = file.Write(data); err != nil {
		file.Close()
		return nil, err
	}
	if err = file.Close(); err != nil {
		return nil, err
	}
	return loadEntriesFromWapp(path)
}

func resolutionModuleVersion(resolution *regapi.DependencyResolution, module string) string {
	for _, selected := range resolution.Modules {
		if selected.Name == module {
			return selected.Version
		}
	}
	return ""
}
