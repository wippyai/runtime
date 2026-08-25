// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/system/registry/topology"
	"github.com/wippyai/wapp"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestDependencyHandler_BaselineTransitionResolvesSharedOverlayGraph(t *testing.T) {
	ctx := newTestContext()
	tmpDir := t.TempDir()
	lockPath := filepath.Join(tmpDir, "wippy.lock")
	vendorDir := filepath.Join(tmpDir, "vendor")

	type selection struct{ module, version string }
	artifacts := map[selection][]byte{
		{"acme/app", "v2.0.0"}: buildWappBytes(t, []wapp.Entry{{
			ID: wapp.NewID("acme.app", "service"), Kind: "service",
		}}),
		{"acme/shared", "v2.0.0"}: buildWappBytes(t, []wapp.Entry{{
			ID: wapp.NewID("acme.shared", "service"), Kind: "service",
		}}),
		{"acme/plugin", "v1.0.0"}: buildWappBytes(t, []wapp.Entry{
			{ID: wapp.NewID("acme.plugin", "service"), Kind: "service"},
			{ID: wapp.NewID("acme.plugin", "shared"), Kind: regapi.NamespaceDependency,
				Data: map[string]any{"component": "acme/shared", "version": ">=v1.0.0"}},
		}),
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
    version: v2.0.0
    hash: %s
    root: true
  - name: acme/shared
    version: v2.0.0
    hash: %s
`, digests[selection{"acme/app", "v2.0.0"}], digests[selection{"acme/shared", "v2.0.0"}])), 0o600))

	manifestCalls := make(map[selection]int)
	hubClient := &fakeHub{
		getManifest: func(_ context.Context, org, module, constraint string) (*ModuleManifest, error) {
			selected := selection{org + "/" + module, constraint}
			artifact, ok := artifacts[selected]
			if !ok {
				return nil, fmt.Errorf("unexpected manifest %s@%s", selected.module, selected.version)
			}
			manifestCalls[selected]++
			manifest := &ModuleManifest{
				Org: org, Name: module, Version: constraint, VersionID: constraint,
				Digest: digests[selected], SizeBytes: uint64(len(artifact)), URL: "memory://" + module + "@" + constraint,
			}
			if selected.module == "acme/app" || selected.module == "acme/plugin" {
				manifest.Dependencies = []ManifestDep{{
					Org: "acme", Name: "shared", Version: "v2.0.0", Constraint: ">=v1.0.0",
					Digest: digests[selection{"acme/shared", "v2.0.0"}],
				}}
			}
			return manifest, nil
		},
		downloadFile: func(_ context.Context, url, destination string) error {
			var selected selection
			switch url {
			case "memory://app@v2.0.0":
				selected = selection{"acme/app", "v2.0.0"}
			case "memory://shared@v2.0.0":
				selected = selection{"acme/shared", "v2.0.0"}
			case "memory://plugin@v1.0.0":
				selected = selection{"acme/plugin", "v1.0.0"}
			default:
				return fmt.Errorf("unexpected download %s", url)
			}
			if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
				return err
			}
			return os.WriteFile(destination, artifacts[selected], 0o600)
		},
	}
	core, logs := observer.New(zap.WarnLevel)
	handler, err := NewDependencyHandler(DependencyHandlerOptions{
		Hub: hubClient, Logger: zap.New(core), Resolver: topology.NewResolver(),
		LockPath: lockPath, VendorDir: vendorDir,
	})
	require.NoError(t, err)

	appRoot := regapi.Entry{
		ID: regapi.NewID("app.deps", "app"), Kind: regapi.NamespaceDependency, DependencyRoot: true,
		Data: payload.New(map[string]any{"component": "acme/app", "version": "v2.0.0"}),
	}
	appService := fixtureOwned(regapi.Entry{
		ID: regapi.NewID("acme.app", "service"), Kind: "service",
	}, "acme/app", "v2.0.0", digests[selection{"acme/app", "v2.0.0"}])
	appShared := fixtureOwned(regapi.Entry{
		ID: regapi.NewID("acme.app", "shared"), Kind: regapi.NamespaceDependency,
		Data: payload.New(map[string]any{"component": "acme/shared", "version": ">=v1.0.0"}),
	}, "acme/app", "v2.0.0", digests[selection{"acme/app", "v2.0.0"}])
	sharedService := fixtureOwned(regapi.Entry{
		ID: regapi.NewID("acme.shared", "service"), Kind: "service",
	}, "acme/shared", "v2.0.0", digests[selection{"acme/shared", "v2.0.0"}])
	baseline := regapi.State{appRoot, appService, appShared, sharedService}
	pluginRoot := regapi.Entry{
		ID: regapi.NewID("user.deps", "plugin"), Kind: regapi.NamespaceDependency,
		Data: payload.New(map[string]any{"component": "acme/plugin", "version": "v1.0.0"}),
	}
	target := append(append(regapi.State(nil), baseline...), pluginRoot)
	oldDigest := func(value string) string {
		sum := sha256.Sum256([]byte(value))
		return "sha256:" + hex.EncodeToString(sum[:])
	}
	stored := (&regapi.DependencyResolution{
		BaselineDigest: "sha256:old-deployment",
		InputDigest: dependencyInputDigest([]desiredDependency{
			{entry: appRoot, definition: DependencyDefinition{Component: "acme/app", Version: "v1.0.0"}},
			{entry: pluginRoot, definition: DependencyDefinition{Component: "acme/plugin", Version: "v1.0.0"}},
		}),
		Roots: []regapi.DependencyRoot{
			{ID: appRoot.ID.String(), Component: "acme/app", Version: "v1.0.0"},
			{ID: pluginRoot.ID.String(), Component: "acme/plugin", Version: "v1.0.0"},
		},
		Modules: []regapi.ResolvedModule{
			{Name: "acme/app", Version: "v1.0.0", Source: moduleSourceHub, Digest: oldDigest("app-v1")},
			{Name: "acme/shared", Version: "v1.0.0", Source: moduleSourceHub, Digest: oldDigest("shared-v1")},
			{Name: "acme/plugin", Version: "v1.0.0", Source: moduleSourceHub, Digest: digests[selection{"acme/plugin", "v1.0.0"}]},
			{Name: "acme/removed", Version: "v1.0.0", Source: moduleSourceHub, Digest: oldDigest("removed-v1")},
		},
	}).Canonical()

	result, err := handler.ReconcileResolution(ctx, fixtureState(baseline), fixtureState(target), stored)
	require.NoError(t, err)
	selected := make([]string, 0, len(result.Resolution.Modules))
	for _, mod := range result.Resolution.Modules {
		selected = append(selected, mod.Name+"@"+mod.Version)
	}
	sort.Strings(selected)
	require.Equal(t, []string{
		"acme/app@v2.0.0", "acme/plugin@v1.0.0", "acme/shared@v2.0.0",
	}, selected, "the new root graph, stable user overlay, and shared pin must be solved together")
	require.NotEqual(t, stored.BaselineDigest, result.Resolution.BaselineDigest)
	require.Len(t, logs.FilterMessage("stored dependency resolution does not match deployment baseline; resolving final declarations").All(), 1)
	require.Equal(t, 1, manifestCalls[selection{"acme/app", "v2.0.0"}])
	require.Equal(t, 1, manifestCalls[selection{"acme/plugin", "v1.0.0"}])
	require.Equal(t, 1, manifestCalls[selection{"acme/shared", "v2.0.0"}])
}

// The lock answers for a module whose records carry no identity of their own.
// It never overrides one: a replacement resident at a tree identity the lock
// cannot know is not a conflict with the stored selection.
func TestLegacyResolutionConflict_LockIsAFallbackNotAnOverride(t *testing.T) {
	ctx := newTestContext()
	tmpDir := t.TempDir()
	lockPath := filepath.Join(tmpDir, "wippy.lock")
	const (
		treeDigest = "sha256-tree-v1:1111111111111111111111111111111111111111111111111111111111111111"
		lockDigest = "sha256:2222222222222222222222222222222222222222222222222222222222222222"
	)
	require.NoError(t, os.WriteFile(lockPath, []byte(fmt.Sprintf(`directories:
  modules: vendor
modules:
  - name: acme/local
    version: v1.0.0
    hash: %s
replace:
  - from: acme/local
    to: ./local
`, lockDigest)), 0o600))

	handler, err := NewDependencyHandler(DependencyHandlerOptions{
		Hub: &fakeHub{}, Logger: zap.NewNop(), LockPath: lockPath,
		VendorDir: filepath.Join(tmpDir, "vendor"),
	})
	require.NoError(t, err)

	root := regapi.Entry{
		ID: regapi.NewID("app.deps", "local"), Kind: regapi.NamespaceDependency,
		Data: payload.New(map[string]any{"component": "acme/local", "version": "v1.0.0"}),
	}
	service := fixtureOwned(regapi.Entry{
		ID: regapi.NewID("acme.local", "service"), Kind: "service",
	}, "acme/local", "v1.0.0", treeDigest)
	baseline := fixtureState(regapi.State{root, service})

	stored := &regapi.DependencyResolution{
		Modules: []regapi.ResolvedModule{{Name: "acme/local", Version: "v1.0.0", Digest: treeDigest}},
	}
	conflict, err := handler.legacyResolutionConflictsWithBaseline(ctx, baseline, stored, payload.GetTranscoder(ctx))
	require.NoError(t, err)
	require.False(t, conflict, "the resident tree identity is authoritative over the lock hash")

	// With no resident digest the lock is the only identity available, and the
	// stored selection is then judged against it.
	unrecorded := fixtureState(regapi.State{root, fixtureOwned(regapi.Entry{
		ID: regapi.NewID("acme.local", "service"), Kind: "service",
	}, "acme/local", "v1.0.0", "")})
	conflict, err = handler.legacyResolutionConflictsWithBaseline(ctx, unrecorded, stored, payload.GetTranscoder(ctx))
	require.NoError(t, err)
	require.True(t, conflict, "without a resident digest the lock hash decides")
}
