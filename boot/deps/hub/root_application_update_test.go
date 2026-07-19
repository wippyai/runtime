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
	workerRootID := regapi.NewID("app.deps", "worker")
	artifacts := map[selection][]byte{
		{"acme/app", "v2.0.0"}: buildWappBytes(t, []wapp.Entry{
			{ID: wapp.NewID("acme.app", "service"), Kind: "service"},
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
	require.NoError(t, os.WriteFile(lockPath, []byte(fmt.Sprintf(`directories:
  modules: vendor
modules:
  - name: acme/app
    version: v1.0.0
    root: true
  - name: acme/worker
    version: v1.0.0
    hash: %s
`, digests[selection{"acme/worker", "v1.0.0"}])), 0o600))

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
		ID: regapi.NewID("app.deps", "app"), Kind: regapi.NamespaceDependency, DependencyRoot: true,
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
}
