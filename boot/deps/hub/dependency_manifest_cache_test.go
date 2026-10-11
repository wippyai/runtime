// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	"go.uber.org/zap"
)

func TestRejectedRootBatchKeepsImmutableManifestAndRetryValid(t *testing.T) {
	ctx := withCurrentResolution(newTestContext(),
		regapi.ResolvedModule{Name: "acme/app", Version: "1.0.0", Source: moduleSourceHub,
			Digest: "sha256:" + lockedResolutionDigest},
		regapi.ResolvedModule{Name: "acme/worker", Version: "1.0.0", Source: moduleSourceHub,
			Digest: "sha256:" + lockedResolutionDigest},
	)
	handler, err := NewDependencyHandler(DependencyHandlerOptions{Logger: zap.NewNop(), Hub: &fakeHub{
		getManifest: func(_ context.Context, org, name, version string) (*ModuleManifest, error) {
			manifest := &ModuleManifest{Org: org, Name: name, Version: version, VersionID: "hub-" + name,
				Digest: "sha256:" + lockedResolutionDigest}
			if name == "app" {
				manifest.Dependencies = []ManifestDep{{Org: "acme", Name: "worker", Version: "1.0.0"}}
			}
			return manifest, nil
		},
		listVersions: func(context.Context, string, string) ([]VersionInfo, error) {
			return []VersionInfo{{Version: "1.0.0"}, {Version: "2.0.0"}}, nil
		},
	}})
	require.NoError(t, err)
	t.Cleanup(handler.manifestCache.Close)
	handler.deployment = &regapi.Deployment{Root: "acme/app"}
	root := regapi.Entry{ID: regapi.NewID("host.deps", "app"), Kind: regapi.NamespaceDependency,
		Data: payload.New(DependencyDefinition{Component: "acme/app", Version: "1.0.0"})}
	worker := ownedEntry(regapi.Entry{ID: regapi.NewID("app.deps", "worker"), Kind: regapi.NamespaceDependency,
		Registry: regapi.EntryMetadata{Root: true},
		Data:     payload.New(DependencyDefinition{Component: "acme/worker", Version: "1.0.0"})}, "acme/app")
	pinned := regapi.Entry{ID: regapi.NewID("host.deps", "worker"), Kind: regapi.NamespaceDependency,
		Data: payload.New(DependencyDefinition{Component: "acme/worker", Version: "1.0.0"})}
	snapshot := regapi.State{root, worker, pinned,
		ownedEntry(regapi.Entry{ID: regapi.NewID("acme.app", "definition"), Kind: regapi.NamespaceDefinition}, "acme/app"),
		ownedEntry(regapi.Entry{ID: regapi.NewID("acme.worker", "definition"), Kind: regapi.NamespaceDefinition}, "acme/worker"),
	}
	candidate := worker
	candidate.Data = payload.New(DependencyDefinition{Component: "acme/worker", Version: "2.0.0"})
	_, err = handler.ExpandChanges(ctx, regapi.ChangeSet{
		{Kind: regapi.EntryUpdate, Entry: candidate},
		{Kind: regapi.EntryUpdate, Entry: root},
	}, snapshot)
	require.Error(t, err, "the candidate contradicts the independently pinned worker")

	manifest, err := handler.manifestCache.GetManifest(ctx, "acme", "app", "1.0.0")
	require.NoError(t, err)
	require.Equal(t, "hub-app", manifest.VersionID)
	require.Equal(t, []ManifestDep{{Org: "acme", Name: "worker", Version: "1.0.0"}}, manifest.Dependencies,
		"provisional batch declarations must never be cached as immutable artifact metadata")
	result, err := Resolve(ctx, handler.manifestCache, []DependencySpec{
		{Org: "acme", Name: "app", Constraint: "1.0.0"},
		{Org: "acme", Name: "worker", Constraint: "1.0.0"},
	}, nil)
	require.NoError(t, err)
	require.Empty(t, result.Errors, "the same handler must still resolve the committed graph")
}
