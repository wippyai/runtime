// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeManifestProvider struct {
	manifests      map[string]*ModuleManifest
	versions       map[string][]VersionInfo
	getManifest    map[string]int
	listAllVersion map[string]int
}

func newFakeProvider() *fakeManifestProvider {
	return &fakeManifestProvider{
		manifests:      make(map[string]*ModuleManifest),
		versions:       make(map[string][]VersionInfo),
		getManifest:    make(map[string]int),
		listAllVersion: make(map[string]int),
	}
}

func (f *fakeManifestProvider) addModule(org, name, version string, deps ...ManifestDep) {
	key := org + "/" + name + "@" + version
	f.manifests[key] = &ModuleManifest{
		Org:          org,
		Name:         name,
		Version:      version,
		Digest:       "sha256:" + version,
		Dependencies: deps,
	}

	vKey := org + "/" + name
	f.versions[vKey] = append(f.versions[vKey], VersionInfo{
		Version: version,
	})
}

func (f *fakeManifestProvider) GetManifest(_ context.Context, org, module, constraint string) (*ModuleManifest, error) {
	f.getManifest[org+"/"+module]++
	key := org + "/" + module + "@" + constraint
	if m, ok := f.manifests[key]; ok {
		return m, nil
	}
	return nil, fmt.Errorf("module %s/%s@%s not found", org, module, constraint)
}

func (f *fakeManifestProvider) ListAllVersions(_ context.Context, org, module string) ([]VersionInfo, error) {
	f.listAllVersion[org+"/"+module]++
	key := org + "/" + module
	if v, ok := f.versions[key]; ok {
		return v, nil
	}
	return nil, fmt.Errorf("module %s/%s not found", org, module)
}

func TestResolve_SingleRoot(t *testing.T) {
	p := newFakeProvider()
	p.addModule("acme", "http", "1.0.0")

	result, err := Resolve(context.Background(), p, []DependencySpec{
		{Org: "acme", Name: "http", Constraint: "1.0.0"},
	}, nil)

	require.NoError(t, err)
	require.Len(t, result.Modules, 1)
	assert.Empty(t, result.Errors)
	assert.Equal(t, "acme", result.Modules[0].Org)
	assert.Equal(t, "http", result.Modules[0].Name)
	assert.Equal(t, "1.0.0", result.Modules[0].Version)
}

func TestResolve_MultipleRoots(t *testing.T) {
	p := newFakeProvider()
	for i := 0; i < 30; i++ {
		p.addModule("acme", fmt.Sprintf("mod%d", i), "1.0.0")
	}

	roots := make([]DependencySpec, 30)
	for i := range roots {
		roots[i] = DependencySpec{Org: "acme", Name: fmt.Sprintf("mod%d", i), Constraint: "1.0.0"}
	}

	result, err := Resolve(context.Background(), p, roots, nil)
	require.NoError(t, err)
	assert.Len(t, result.Modules, 30)
	assert.Empty(t, result.Errors)
}

func TestResolve_TransitiveDeps(t *testing.T) {
	p := newFakeProvider()
	p.addModule("acme", "app", "1.0.0", ManifestDep{Org: "acme", Name: "lib", Version: "2.0.0"})
	p.addModule("acme", "lib", "2.0.0", ManifestDep{Org: "acme", Name: "core", Version: "3.0.0"})
	p.addModule("acme", "core", "3.0.0")

	result, err := Resolve(context.Background(), p, []DependencySpec{
		{Org: "acme", Name: "app", Constraint: "1.0.0"},
	}, nil)

	require.NoError(t, err)
	assert.Len(t, result.Modules, 3)
	assert.Empty(t, result.Errors)
}

func TestResolve_DiamondDeps(t *testing.T) {
	p := newFakeProvider()
	p.addModule("acme", "app", "1.0.0",
		ManifestDep{Org: "acme", Name: "left", Version: "1.0.0"},
		ManifestDep{Org: "acme", Name: "right", Version: "1.0.0"},
	)
	p.addModule("acme", "left", "1.0.0", ManifestDep{Org: "acme", Name: "shared", Version: "1.0.0"})
	p.addModule("acme", "right", "1.0.0", ManifestDep{Org: "acme", Name: "shared", Version: "1.0.0"})
	p.addModule("acme", "shared", "1.0.0")

	result, err := Resolve(context.Background(), p, []DependencySpec{
		{Org: "acme", Name: "app", Constraint: "1.0.0"},
	}, nil)

	require.NoError(t, err)
	assert.Len(t, result.Modules, 4)
	assert.Empty(t, result.Errors)

	names := make(map[string]bool)
	for _, m := range result.Modules {
		names[m.Name] = true
	}
	assert.True(t, names["shared"], "shared dependency resolved once")
}

func TestResolve_CircularDeps(t *testing.T) {
	p := newFakeProvider()
	p.addModule("acme", "a", "1.0.0", ManifestDep{Org: "acme", Name: "b", Version: "1.0.0"})
	p.addModule("acme", "b", "1.0.0", ManifestDep{Org: "acme", Name: "a", Version: "1.0.0"})

	result, err := Resolve(context.Background(), p, []DependencySpec{
		{Org: "acme", Name: "a", Constraint: "1.0.0"},
	}, nil)

	require.NoError(t, err)
	assert.Len(t, result.Modules, 2)
	assert.Empty(t, result.Errors)
}

func TestResolve_DepthLimit(t *testing.T) {
	p := newFakeProvider()
	p.addModule("acme", "d0", "1.0.0", ManifestDep{Org: "acme", Name: "d1", Version: "1.0.0"})
	p.addModule("acme", "d1", "1.0.0", ManifestDep{Org: "acme", Name: "d2", Version: "1.0.0"})
	p.addModule("acme", "d2", "1.0.0", ManifestDep{Org: "acme", Name: "d3", Version: "1.0.0"})
	p.addModule("acme", "d3", "1.0.0")

	result, err := Resolve(context.Background(), p, []DependencySpec{
		{Org: "acme", Name: "d0", Constraint: "1.0.0"},
	}, &ResolveOptions{MaxDepth: 2})

	require.NoError(t, err)
	assert.Len(t, result.Modules, 2) // d0 and d1
	require.NotEmpty(t, result.Errors)
	assert.Contains(t, result.Errors[0].Message, "maximum dependency depth")
}

func TestResolve_ModuleCountLimit(t *testing.T) {
	p := newFakeProvider()
	for i := 0; i < 5; i++ {
		p.addModule("acme", fmt.Sprintf("m%d", i), "1.0.0")
	}

	roots := make([]DependencySpec, 5)
	for i := range roots {
		roots[i] = DependencySpec{Org: "acme", Name: fmt.Sprintf("m%d", i), Constraint: "1.0.0"}
	}

	result, err := Resolve(context.Background(), p, roots, &ResolveOptions{MaxModules: 3})
	require.NoError(t, err)
	assert.Len(t, result.Modules, 3)
	assert.NotEmpty(t, result.Errors)
	assert.Contains(t, result.Errors[0].Message, "maximum module count")
}

func TestResolve_SemverConstraint(t *testing.T) {
	p := newFakeProvider()
	p.addModule("acme", "lib", "1.0.0")
	p.addModule("acme", "lib", "1.5.0")
	p.addModule("acme", "lib", "2.0.0")

	result, err := Resolve(context.Background(), p, []DependencySpec{
		{Org: "acme", Name: "lib", Constraint: "^1.0.0"},
	}, nil)

	require.NoError(t, err)
	require.Len(t, result.Modules, 1)
	assert.Equal(t, "1.5.0", result.Modules[0].Version)
}

func TestResolve_PrefersLockedTransitiveVersionWhenConstraintAllowsIt(t *testing.T) {
	p := newFakeProvider()
	p.addModule("acme", "app", "1.0.0", ManifestDep{Org: "acme", Name: "lib", Version: ">=1.0.0"})
	p.addModule("acme", "lib", "1.0.0")
	p.addModule("acme", "lib", "1.5.0")

	result, err := Resolve(context.Background(), p, []DependencySpec{
		{Org: "acme", Name: "app", Constraint: "1.0.0"},
	}, &ResolveOptions{
		LockedVersions: map[string]string{
			"acme/lib": "1.0.0",
		},
	})

	require.NoError(t, err)
	assert.Empty(t, result.Errors)
	require.Len(t, result.Modules, 2)
	assert.Equal(t, "app", result.Modules[0].Name)
	assert.Equal(t, "lib", result.Modules[1].Name)
	assert.Equal(t, "1.0.0", result.Modules[1].Version)
}

func TestResolve_IgnoresLockedTransitiveVersionWhenConstraintRejectsIt(t *testing.T) {
	p := newFakeProvider()
	p.addModule("acme", "app", "1.0.0", ManifestDep{Org: "acme", Name: "lib", Version: ">=1.2.0"})
	p.addModule("acme", "lib", "1.0.0")
	p.addModule("acme", "lib", "1.5.0")

	result, err := Resolve(context.Background(), p, []DependencySpec{
		{Org: "acme", Name: "app", Constraint: "1.0.0"},
	}, &ResolveOptions{
		LockedVersions: map[string]string{
			"acme/lib": "1.0.0",
		},
	})

	require.NoError(t, err)
	assert.Empty(t, result.Errors)
	require.Len(t, result.Modules, 2)
	assert.Equal(t, "lib", result.Modules[1].Name)
	assert.Equal(t, "1.5.0", result.Modules[1].Version)
}

func TestResolve_TildeConstraint(t *testing.T) {
	p := newFakeProvider()
	p.addModule("acme", "lib", "1.2.0")
	p.addModule("acme", "lib", "1.2.5")
	p.addModule("acme", "lib", "1.3.0")

	result, err := Resolve(context.Background(), p, []DependencySpec{
		{Org: "acme", Name: "lib", Constraint: "~1.2.0"},
	}, nil)

	require.NoError(t, err)
	require.Len(t, result.Modules, 1)
	assert.Equal(t, "1.2.5", result.Modules[0].Version)
}

func TestResolve_WildcardConstraint(t *testing.T) {
	p := newFakeProvider()
	p.addModule("acme", "lib", "1.0.0")
	p.addModule("acme", "lib", "1.9.0")
	p.addModule("acme", "lib", "2.0.0")

	result, err := Resolve(context.Background(), p, []DependencySpec{
		{Org: "acme", Name: "lib", Constraint: "1.*"},
	}, nil)

	require.NoError(t, err)
	require.Len(t, result.Modules, 1)
	assert.Equal(t, "1.9.0", result.Modules[0].Version)
}

func TestResolve_LabelConstraint(t *testing.T) {
	p := newFakeProvider()
	p.manifests["acme/lib@@latest"] = &ModuleManifest{
		Org: "acme", Name: "lib", Version: "2.0.0",
	}

	result, err := Resolve(context.Background(), p, []DependencySpec{
		{Org: "acme", Name: "lib", Constraint: "@latest"},
	}, nil)

	require.NoError(t, err)
	require.Len(t, result.Modules, 1)
	assert.Equal(t, "2.0.0", result.Modules[0].Version)
}

func TestResolve_EmptyConstraint(t *testing.T) {
	p := newFakeProvider()
	p.manifests["acme/lib@"] = &ModuleManifest{
		Org: "acme", Name: "lib", Version: "3.0.0",
	}

	result, err := Resolve(context.Background(), p, []DependencySpec{
		{Org: "acme", Name: "lib", Constraint: ""},
	}, nil)

	require.NoError(t, err)
	require.Len(t, result.Modules, 1)
	assert.Equal(t, "3.0.0", result.Modules[0].Version)
}

func TestResolve_ModuleNotFound(t *testing.T) {
	p := newFakeProvider()

	result, err := Resolve(context.Background(), p, []DependencySpec{
		{Org: "acme", Name: "missing", Constraint: "1.0.0"},
	}, nil)

	require.NoError(t, err)
	assert.Empty(t, result.Modules)
	require.Len(t, result.Errors, 1)
	assert.Equal(t, "acme", result.Errors[0].Org)
	assert.Equal(t, "missing", result.Errors[0].Name)
}

func TestResolve_NoMatchingVersion(t *testing.T) {
	p := newFakeProvider()
	p.addModule("acme", "lib", "1.0.0")

	result, err := Resolve(context.Background(), p, []DependencySpec{
		{Org: "acme", Name: "lib", Constraint: "^5.0.0"},
	}, nil)

	require.NoError(t, err)
	assert.Empty(t, result.Modules)
	require.Len(t, result.Errors, 1)
	assert.Contains(t, result.Errors[0].Message, "no available version of acme/lib")
	assert.NotContains(t, result.Errors[0].Message, "conflicting version constraints")
}

func TestResolve_PartialSuccess(t *testing.T) {
	p := newFakeProvider()
	p.addModule("acme", "http", "1.0.0")

	result, err := Resolve(context.Background(), p, []DependencySpec{
		{Org: "acme", Name: "http", Constraint: "1.0.0"},
		{Org: "acme", Name: "missing", Constraint: "1.0.0"},
	}, nil)

	require.NoError(t, err)
	assert.Len(t, result.Modules, 1)
	assert.Len(t, result.Errors, 1)
	assert.Equal(t, "http", result.Modules[0].Name)
	assert.Equal(t, "missing", result.Errors[0].Name)
}

func TestResolve_CancelledContext(t *testing.T) {
	p := newFakeProvider()
	p.addModule("acme", "lib", "1.0.0")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := Resolve(ctx, p, []DependencySpec{
		{Org: "acme", Name: "lib", Constraint: "1.0.0"},
	}, nil)

	require.Error(t, err)
}

func TestResolve_ExactVersionPassedDirectly(t *testing.T) {
	p := newFakeProvider()
	p.addModule("acme", "lib", "v1.2.3")

	result, err := Resolve(context.Background(), p, []DependencySpec{
		{Org: "acme", Name: "lib", Constraint: "v1.2.3"},
	}, nil)

	require.NoError(t, err)
	require.Len(t, result.Modules, 1)
	assert.Equal(t, "v1.2.3", result.Modules[0].Version)
}

func TestResolve_SemverPreservesOriginalVersionString(t *testing.T) {
	p := newFakeProvider()
	// Versions stored with v prefix
	p.addModule("acme", "lib", "v1.0.0")
	p.addModule("acme", "lib", "v1.5.0")
	p.addModule("acme", "lib", "v2.0.0")

	result, err := Resolve(context.Background(), p, []DependencySpec{
		{Org: "acme", Name: "lib", Constraint: "^1.0.0"},
	}, nil)

	require.NoError(t, err)
	require.Len(t, result.Modules, 1)
	assert.Equal(t, "v1.5.0", result.Modules[0].Version, "must preserve original v prefix")
}

func TestResolve_ExactEqualConstraintPassesThroughDirectly(t *testing.T) {
	p := newFakeProvider()
	// Only register the manifest for the exact version, no versions list needed
	p.manifests["acme/lib@v1.0.0"] = &ModuleManifest{
		Org: "acme", Name: "lib", Version: "v1.0.0",
	}

	result, err := Resolve(context.Background(), p, []DependencySpec{
		{Org: "acme", Name: "lib", Constraint: "=v1.0.0"},
	}, nil)

	require.NoError(t, err)
	require.Len(t, result.Modules, 1)
	assert.Equal(t, "v1.0.0", result.Modules[0].Version)
}

func TestResolve_PreservesModuleMetadata(t *testing.T) {
	p := newFakeProvider()
	p.manifests["acme/lib@1.0.0"] = &ModuleManifest{
		Org:       "acme",
		Name:      "lib",
		Version:   "1.0.0",
		VersionID: "vid-123",
		Digest:    "sha256:abc",
		SizeBytes: 4096,
		Protected: true,
		URL:       "https://example.com/lib.wapp",
	}

	result, err := Resolve(context.Background(), p, []DependencySpec{
		{Org: "acme", Name: "lib", Constraint: "1.0.0"},
	}, nil)

	require.NoError(t, err)
	require.Len(t, result.Modules, 1)
	m := result.Modules[0]
	assert.Equal(t, "vid-123", m.VersionID)
	assert.Equal(t, "sha256:abc", m.Digest)
	assert.Equal(t, uint64(4096), m.SizeBytes)
	assert.True(t, m.Protected)
	assert.Equal(t, "https://example.com/lib.wapp", m.URL)
}

type mutableProvider struct {
	fakeManifestProvider
	overrides map[string]*ModuleManifest
}

func newMutableProvider() *mutableProvider {
	return &mutableProvider{
		fakeManifestProvider: *newFakeProvider(),
		overrides:            make(map[string]*ModuleManifest),
	}
}

func (m *mutableProvider) override(org, name, version, digest string) {
	key := org + "/" + name + "@" + version
	m.overrides[key] = &ModuleManifest{
		Org:     org,
		Name:    name,
		Version: version,
		Digest:  digest,
	}
}

func (m *mutableProvider) GetManifest(ctx context.Context, org, module, constraint string) (*ModuleManifest, error) {
	key := org + "/" + module + "@" + constraint
	if mm, ok := m.overrides[key]; ok {
		return mm, nil
	}
	return m.fakeManifestProvider.GetManifest(ctx, org, module, constraint)
}

func TestResolve_LockedDigestMatch(t *testing.T) {
	p := newFakeProvider()
	p.addModule("acme", "http", "1.0.0")

	result, err := Resolve(context.Background(), p, []DependencySpec{
		{Org: "acme", Name: "http", Constraint: "1.0.0"},
	}, &ResolveOptions{
		LockedDigests: map[string]string{"acme/http@1.0.0": "sha256:1.0.0"},
	})

	require.NoError(t, err)
	assert.Empty(t, result.Errors)
	require.Len(t, result.Modules, 1)
	assert.Equal(t, "sha256:1.0.0", result.Modules[0].Digest)
}

func TestResolve_LockedDigestMatchAcceptsBareHubDigest(t *testing.T) {
	p := newMutableProvider()
	p.addModule("acme", "http", "1.0.0")
	p.override("acme", "http", "1.0.0", "abcdef1234")

	result, err := Resolve(context.Background(), p, []DependencySpec{
		{Org: "acme", Name: "http", Constraint: "1.0.0"},
	}, &ResolveOptions{
		LockedDigests: map[string]string{"acme/http@1.0.0": "sha256:abcdef1234"},
	})

	require.NoError(t, err)
	assert.Empty(t, result.Errors)
	require.Len(t, result.Modules, 1)
	assert.Equal(t, "abcdef1234", result.Modules[0].Digest)
}

func TestResolve_LockedDigestMismatchBareProvider(t *testing.T) {
	p := newFakeProvider()
	p.addModule("acme", "http", "1.0.0")

	result, err := Resolve(context.Background(), p, []DependencySpec{
		{Org: "acme", Name: "http", Constraint: "1.0.0"},
	}, &ResolveOptions{
		LockedDigests: map[string]string{"acme/http@1.0.0": "sha256:expected"},
	})

	require.NoError(t, err)
	require.Empty(t, result.Modules)
	require.Len(t, result.Errors, 1)
	assert.Contains(t, result.Errors[0].Message, "manifest digest mismatch")
	assert.Contains(t, result.Errors[0].Message, "sha256:expected")
	assert.Contains(t, result.Errors[0].Message, "sha256:1.0.0")
}

func TestResolve_LockedDigestCacheHeals(t *testing.T) {
	p := newMutableProvider()
	p.addModule("acme", "http", "1.0.0")

	cache := NewManifestCache(p)
	_ = cache.store.Set("acme/http@1.0.0", &ModuleManifest{
		Org: "acme", Name: "http", Version: "1.0.0", Digest: "sha256:stale",
	})

	result, err := Resolve(context.Background(), cache, []DependencySpec{
		{Org: "acme", Name: "http", Constraint: "1.0.0"},
	}, &ResolveOptions{
		LockedDigests: map[string]string{"acme/http@1.0.0": "sha256:1.0.0"},
	})

	require.NoError(t, err)
	assert.Empty(t, result.Errors)
	require.Len(t, result.Modules, 1)
	assert.Equal(t, "sha256:1.0.0", result.Modules[0].Digest)
}

func TestResolve_LockedDigestCacheDriftErrors(t *testing.T) {
	p := newMutableProvider()
	p.addModule("acme", "http", "1.0.0")

	cache := NewManifestCache(p)
	_ = cache.store.Set("acme/http@1.0.0", &ModuleManifest{
		Org: "acme", Name: "http", Version: "1.0.0", Digest: "sha256:stale",
	})
	p.override("acme", "http", "1.0.0", "sha256:drift")

	result, err := Resolve(context.Background(), cache, []DependencySpec{
		{Org: "acme", Name: "http", Constraint: "1.0.0"},
	}, &ResolveOptions{
		LockedDigests: map[string]string{"acme/http@1.0.0": "sha256:expected"},
	})

	require.NoError(t, err)
	require.Empty(t, result.Modules)
	require.Len(t, result.Errors, 1)
	assert.Contains(t, result.Errors[0].Message, "manifest digest mismatch")
	assert.Contains(t, result.Errors[0].Message, "sha256:drift")
}

func TestResolve_LockedDigestMissingKeyDoesNotValidate(t *testing.T) {
	p := newFakeProvider()
	p.addModule("acme", "http", "1.0.0")

	result, err := Resolve(context.Background(), p, []DependencySpec{
		{Org: "acme", Name: "http", Constraint: "1.0.0"},
	}, &ResolveOptions{
		LockedDigests: map[string]string{"other/module": "sha256:irrelevant"},
	})

	require.NoError(t, err)
	assert.Empty(t, result.Errors)
	require.Len(t, result.Modules, 1)
}

func TestResolve_IncompatibleConstraintsAcrossRootsFailLoud(t *testing.T) {
	p := newFakeProvider()
	p.addModule("acme", "a", "1.0.0", ManifestDep{Org: "acme", Name: "x", Version: "^1.0.0"})
	p.addModule("acme", "b", "1.0.0", ManifestDep{Org: "acme", Name: "x", Version: ">=2.0.0"})
	p.addModule("acme", "x", "1.5.0")
	p.addModule("acme", "x", "2.0.0")

	roots := []DependencySpec{
		{Org: "acme", Name: "a", Constraint: "1.0.0"},
		{Org: "acme", Name: "b", Constraint: "1.0.0"},
	}

	result, err := Resolve(context.Background(), p, roots, nil)
	require.NoError(t, err)

	var conflict *ResolutionError
	for i := range result.Errors {
		if result.Errors[i].Name == "x" {
			conflict = &result.Errors[i]
			break
		}
	}
	require.NotNil(t, conflict, "incompatible constraints must surface a conflict naming module x")
	assert.Contains(t, conflict.Message, "acme/x")
	assert.Contains(t, conflict.Message, "^1.0.0")
	assert.Contains(t, conflict.Message, ">=2.0.0")
	assert.Contains(t, conflict.Message, "acme/a")
	assert.Contains(t, conflict.Message, "acme/b")
}

func TestResolve_CompatibleConstraintsAcrossRootsAreOrderIndependent(t *testing.T) {
	build := func() *fakeManifestProvider {
		p := newFakeProvider()
		p.addModule("acme", "a", "1.0.0", ManifestDep{Org: "acme", Name: "x", Version: "^1.0.0"})
		p.addModule("acme", "b", "1.0.0", ManifestDep{Org: "acme", Name: "x", Version: ">=1.2.0"})
		p.addModule("acme", "x", "1.0.0")
		p.addModule("acme", "x", "1.5.0")
		p.addModule("acme", "x", "2.0.0")
		return p
	}

	versionOf := func(res *ResolveDependenciesResult) string {
		for _, m := range res.Modules {
			if m.Name == "x" {
				return m.Version
			}
		}
		return ""
	}

	rootA := DependencySpec{Org: "acme", Name: "a", Constraint: "1.0.0"}
	rootB := DependencySpec{Org: "acme", Name: "b", Constraint: "1.0.0"}

	ab, err := Resolve(context.Background(), build(), []DependencySpec{rootA, rootB}, nil)
	require.NoError(t, err)
	assert.Empty(t, ab.Errors)

	ba, err := Resolve(context.Background(), build(), []DependencySpec{rootB, rootA}, nil)
	require.NoError(t, err)
	assert.Empty(t, ba.Errors)

	assert.Equal(t, "1.5.0", versionOf(ab), "must pick the highest version satisfying both constraints")
	assert.Equal(t, versionOf(ab), versionOf(ba), "resolved version must be independent of walk order")
}

func TestResolve_ConstraintCompatibilityMatrix(t *testing.T) {
	tests := []struct {
		name       string
		c1         string
		c2         string
		compatible bool
	}{
		{name: "compatible carets same major", c1: "^1.2.0", c2: "^1.3.0", compatible: true},
		{name: "incompatible carets different major", c1: "^1.0.0", c2: "^2.0.0", compatible: false},
		{name: "compatible tildes same minor", c1: "~1.2.0", c2: "~1.2.3", compatible: true},
		{name: "incompatible tildes different minor", c1: "~1.2.0", c2: "~1.3.0", compatible: false},
		{name: "compatible ranges with overlap", c1: ">=1.0.0 <2.0.0", c2: ">=1.5.0 <3.0.0", compatible: true},
		{name: "incompatible ranges no overlap", c1: ">=1.0.0 <2.0.0", c2: ">=2.0.0 <3.0.0", compatible: false},
		{name: "exact different versions", c1: "1.2.3", c2: "1.2.4", compatible: false},
		{name: "wildcard with anything", c1: "*", c2: "^1.0.0", compatible: true},
	}

	pool := []string{
		"0.9.0", "1.2.0", "1.2.3", "1.2.4", "1.2.5",
		"1.3.0", "1.5.0", "1.9.0", "2.0.0", "2.5.3", "3.0.0",
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := newFakeProvider()
			p.addModule("acme", "a", "1.0.0", ManifestDep{Org: "acme", Name: "x", Version: tt.c1})
			p.addModule("acme", "b", "1.0.0", ManifestDep{Org: "acme", Name: "x", Version: tt.c2})
			for _, v := range pool {
				p.addModule("acme", "x", v)
			}

			result, err := Resolve(context.Background(), p, []DependencySpec{
				{Org: "acme", Name: "a", Constraint: "1.0.0"},
				{Org: "acme", Name: "b", Constraint: "1.0.0"},
			}, nil)
			require.NoError(t, err)

			hasConflict := false
			for _, e := range result.Errors {
				if e.Name == "x" {
					hasConflict = true
				}
			}

			if tt.compatible {
				assert.False(t, hasConflict, "constraints %q and %q must resolve to a shared version", tt.c1, tt.c2)
			} else {
				assert.True(t, hasConflict, "constraints %q and %q must fail loud as a conflict", tt.c1, tt.c2)
			}
		})
	}
}

func TestResolve_NarrowingRetractsSupersededSubtree(t *testing.T) {
	build := func() *fakeManifestProvider {
		p := newFakeProvider()
		// a alone would resolve x to its highest (1.9.0); b pins x to 1.0.0.
		p.addModule("acme", "a", "1.0.0", ManifestDep{Org: "acme", Name: "x", Version: ">=1.0.0"})
		p.addModule("acme", "b", "1.0.0", ManifestDep{Org: "acme", Name: "x", Version: "1.0.0"})
		// x@1.9.0 pulls y ^2, x@1.0.0 pulls y ^1: the superseded y demand must be retracted.
		p.addModule("acme", "x", "1.9.0", ManifestDep{Org: "acme", Name: "y", Version: "^2.0.0"})
		p.addModule("acme", "x", "1.0.0", ManifestDep{Org: "acme", Name: "y", Version: "^1.0.0"})
		p.addModule("acme", "y", "1.5.0")
		p.addModule("acme", "y", "2.5.0")
		return p
	}

	versionOf := func(res *ResolveDependenciesResult, name string) string {
		for _, m := range res.Modules {
			if m.Name == name {
				return m.Version
			}
		}
		return ""
	}
	set := func(res *ResolveDependenciesResult) map[string]string {
		out := make(map[string]string)
		for _, m := range res.Modules {
			out[m.Name] = m.Version
		}
		return out
	}

	rootA := DependencySpec{Org: "acme", Name: "a", Constraint: "1.0.0"}
	rootB := DependencySpec{Org: "acme", Name: "b", Constraint: "1.0.0"}

	ab, err := Resolve(context.Background(), build(), []DependencySpec{rootA, rootB}, nil)
	require.NoError(t, err)
	assert.Empty(t, ab.Errors, "retracting the x@1.9 subtree must clear the stale y ^2.0.0 demand")

	ba, err := Resolve(context.Background(), build(), []DependencySpec{rootB, rootA}, nil)
	require.NoError(t, err)
	assert.Empty(t, ba.Errors)

	assert.Equal(t, "1.0.0", versionOf(ab, "x"))
	assert.Equal(t, "1.5.0", versionOf(ab, "y"), "y must follow x@1.0.0's ^1.0.0 demand, not the retracted ^2.0.0")
	assert.Equal(t, set(ab), set(ba), "resolved set must be independent of walk order")
}

func TestResolve_DirectIncompatibilityFailsLoud(t *testing.T) {
	p := newFakeProvider()
	p.addModule("acme", "a", "1.0.0", ManifestDep{Org: "acme", Name: "x", Version: "^1.0.0"})
	p.addModule("acme", "b", "1.0.0", ManifestDep{Org: "acme", Name: "x", Version: "^2.0.0"})
	p.addModule("acme", "x", "1.5.0")
	p.addModule("acme", "x", "2.5.0")

	result, err := Resolve(context.Background(), p, []DependencySpec{
		{Org: "acme", Name: "a", Constraint: "1.0.0"},
		{Org: "acme", Name: "b", Constraint: "1.0.0"},
	}, nil)
	require.NoError(t, err)

	var conflict *ResolutionError
	for i := range result.Errors {
		if result.Errors[i].Name == "x" {
			conflict = &result.Errors[i]
			break
		}
	}
	require.NotNil(t, conflict, "non-overlapping ^1 and ^2 must fail loud")
	assert.Contains(t, conflict.Message, "acme/x")
	assert.Contains(t, conflict.Message, "^1.0.0")
	assert.Contains(t, conflict.Message, "^2.0.0")
}

func TestResolve_UnchangedRevisitMakesNoProviderCall(t *testing.T) {
	p := newFakeProvider()
	// shared is reached first via left (level 2) and again via mid->deep (level 3),
	// both with the same semver constraint, so the second visit changes nothing.
	p.addModule("acme", "app", "1.0.0",
		ManifestDep{Org: "acme", Name: "left", Version: "1.0.0"},
		ManifestDep{Org: "acme", Name: "mid", Version: "1.0.0"},
	)
	p.addModule("acme", "left", "1.0.0", ManifestDep{Org: "acme", Name: "shared", Version: "^1.0.0"})
	p.addModule("acme", "mid", "1.0.0", ManifestDep{Org: "acme", Name: "deep", Version: "1.0.0"})
	p.addModule("acme", "deep", "1.0.0", ManifestDep{Org: "acme", Name: "shared", Version: "^1.0.0"})
	p.addModule("acme", "shared", "1.0.0")
	p.addModule("acme", "shared", "1.2.0")

	result, err := Resolve(context.Background(), p, []DependencySpec{
		{Org: "acme", Name: "app", Constraint: "1.0.0"},
	}, nil)
	require.NoError(t, err)
	assert.Empty(t, result.Errors)

	assert.Equal(t, 1, p.listAllVersion["acme/shared"],
		"unchanged revisit must short-circuit without re-listing versions")
	assert.Equal(t, 1, p.getManifest["acme/shared"],
		"unchanged revisit must not refetch the manifest")
}

func TestResolve_LabelWithDivergentSemverConflicts(t *testing.T) {
	// A label combined with a divergent semver range cannot be intersected against
	// available versions and is reported as a conflict rather than silently favoring one.
	p := newFakeProvider()
	p.addModule("acme", "a", "1.0.0", ManifestDep{Org: "acme", Name: "x", Version: "@latest"})
	p.addModule("acme", "b", "1.0.0", ManifestDep{Org: "acme", Name: "x", Version: "^1.0.0"})
	p.addModule("acme", "x", "1.5.0")

	result, err := Resolve(context.Background(), p, []DependencySpec{
		{Org: "acme", Name: "a", Constraint: "1.0.0"},
		{Org: "acme", Name: "b", Constraint: "1.0.0"},
	}, nil)
	require.NoError(t, err)

	var conflict *ResolutionError
	for _, e := range result.Errors {
		if e.Name == "x" {
			conflict = &e
			break
		}
	}
	require.NotNil(t, conflict, "label vs divergent semver is not deterministically intersectable")
	assert.Contains(t, conflict.Message, "conflicting version constraints for acme/x")
	assert.Contains(t, conflict.Message, "@latest")
	assert.Contains(t, conflict.Message, "^1.0.0")
}

func TestResolve_MaxDepthEnforcedAgainstLiveDepthAfterRetraction(t *testing.T) {
	moduleNames := func(res *ResolveDependenciesResult) map[string]string {
		out := make(map[string]string)
		for _, m := range res.Modules {
			out[m.Name] = m.Version
		}
		return out
	}
	depthError := func(res *ResolveDependenciesResult, name string) *ResolutionError {
		for i := range res.Errors {
			if res.Errors[i].Name == name && strings.Contains(res.Errors[i].Message, "maximum dependency depth") {
				return &res.Errors[i]
			}
		}
		return nil
	}

	t.Run("rejected when only the over-depth path stays live", func(t *testing.T) {
		p := newFakeProvider()
		// flip@2.0.0 pulls m at a shallow depth (2); the deep chain pulls m at depth 3.
		// c2 (depth 2) later narrows flip to 1.0.0, retracting the shallow edge, so m is
		// reachable only through the over-depth path and must be rejected against live depth.
		p.addModule("acme", "r", "1.0.0",
			ManifestDep{Org: "acme", Name: "flip", Version: ">=1.0.0"},
			ManifestDep{Org: "acme", Name: "chain", Version: "1.0.0"},
		)
		p.addModule("acme", "flip", "1.0.0")
		p.addModule("acme", "flip", "2.0.0", ManifestDep{Org: "acme", Name: "m", Version: "1.0.0"})
		p.addModule("acme", "chain", "1.0.0", ManifestDep{Org: "acme", Name: "c2", Version: "1.0.0"})
		p.addModule("acme", "c2", "1.0.0",
			ManifestDep{Org: "acme", Name: "m", Version: "1.0.0"},
			ManifestDep{Org: "acme", Name: "flip", Version: "1.0.0"},
		)
		p.addModule("acme", "m", "1.0.0")

		result, err := Resolve(context.Background(), p, []DependencySpec{
			{Org: "acme", Name: "r", Constraint: "1.0.0"},
		}, &ResolveOptions{MaxDepth: 3})
		require.NoError(t, err)

		mods := moduleNames(result)
		assert.Equal(t, "1.0.0", mods["flip"], "flip must narrow to 1.0.0, dropping its shallow edge to m")
		assert.NotContains(t, mods, "m", "m must be rejected once only the over-depth path remains")
		require.NotNil(t, depthError(result, "m"), "live depth must reject m for exceeding max depth")
	})

	t.Run("kept when a shallow path stays live", func(t *testing.T) {
		p := newFakeProvider()
		// stable pulls m at depth 2 and never narrows, so the shallow edge persists.
		// flipD@2.0.0 pulls deepmid which redundantly pulls m at depth 3. An independent
		// subtree (pinner -> subpin) narrows flipD to 1.0.0, retracting the deep path -
		// m must survive on the live shallow path.
		p.addModule("acme", "r", "1.0.0",
			ManifestDep{Org: "acme", Name: "stable", Version: "1.0.0"},
			ManifestDep{Org: "acme", Name: "flipd", Version: ">=1.0.0"},
			ManifestDep{Org: "acme", Name: "pinner", Version: "1.0.0"},
		)
		p.addModule("acme", "stable", "1.0.0", ManifestDep{Org: "acme", Name: "m", Version: "1.0.0"})
		p.addModule("acme", "flipd", "1.0.0")
		p.addModule("acme", "flipd", "2.0.0", ManifestDep{Org: "acme", Name: "deepmid", Version: "1.0.0"})
		p.addModule("acme", "deepmid", "1.0.0", ManifestDep{Org: "acme", Name: "m", Version: "1.0.0"})
		p.addModule("acme", "pinner", "1.0.0", ManifestDep{Org: "acme", Name: "subpin", Version: "1.0.0"})
		p.addModule("acme", "subpin", "1.0.0", ManifestDep{Org: "acme", Name: "flipd", Version: "1.0.0"})
		p.addModule("acme", "m", "1.0.0")

		result, err := Resolve(context.Background(), p, []DependencySpec{
			{Org: "acme", Name: "r", Constraint: "1.0.0"},
		}, &ResolveOptions{MaxDepth: 3})
		require.NoError(t, err)

		mods := moduleNames(result)
		assert.Equal(t, "1.0.0", mods["flipd"], "flipd must narrow to 1.0.0 via the independent subtree")
		assert.Equal(t, "1.0.0", mods["m"], "m must survive on its live shallow path")
		assert.Nil(t, depthError(result, "m"), "m must not be rejected while a shallow path stays live")
		assert.NotContains(t, mods, "deepmid", "the retracted deep path must be gone")
	})
}

func TestResolve_DepthPropagatesWhenParentVersionUnchanged(t *testing.T) {
	// p (only version 1.0.0) is reached via a shallow flip parent (constraint "1.0.0",
	// depth 2) and via a deep chain (constraint ">=1.0.0", depth 3); it resolves to 1.0.0
	// with child c accepted at depth 3. An independent narrower flips the shallow parent,
	// retracting p's shallow source: p's live constraints change (so it re-resolves) but
	// its version stays 1.0.0 (v == version) while its effective depth rises 2 -> 3. That
	// new depth must propagate to c, pushing c to depth 4 and over the limit.
	build := func() *fakeManifestProvider {
		p := newFakeProvider()
		p.addModule("acme", "sp", "1.0.0")
		p.addModule("acme", "sp", "2.0.0", ManifestDep{Org: "acme", Name: "p", Version: "1.0.0"})
		p.addModule("acme", "deepchain", "1.0.0", ManifestDep{Org: "acme", Name: "deepmid", Version: "1.0.0"})
		p.addModule("acme", "deepmid", "1.0.0", ManifestDep{Org: "acme", Name: "p", Version: ">=1.0.0"})
		p.addModule("acme", "p", "1.0.0", ManifestDep{Org: "acme", Name: "c", Version: "1.0.0"})
		p.addModule("acme", "c", "1.0.0")
		p.addModule("acme", "narrower", "1.0.0", ManifestDep{Org: "acme", Name: "subnarrow", Version: "1.0.0"})
		p.addModule("acme", "subnarrow", "1.0.0", ManifestDep{Org: "acme", Name: "sp", Version: "1.0.0"})
		return p
	}

	moduleNames := func(res *ResolveDependenciesResult) map[string]string {
		out := make(map[string]string)
		for _, m := range res.Modules {
			out[m.Name] = m.Version
		}
		return out
	}
	hasDepthError := func(res *ResolveDependenciesResult, name string) bool {
		for i := range res.Errors {
			if res.Errors[i].Name == name && strings.Contains(res.Errors[i].Message, "maximum dependency depth") {
				return true
			}
		}
		return false
	}

	rootSP := DependencySpec{Org: "acme", Name: "sp", Constraint: ">=1.0.0"}
	rootDeep := DependencySpec{Org: "acme", Name: "deepchain", Constraint: "1.0.0"}
	rootNarrow := DependencySpec{Org: "acme", Name: "narrower", Constraint: "1.0.0"}

	forward, err := Resolve(context.Background(), build(),
		[]DependencySpec{rootSP, rootDeep, rootNarrow}, &ResolveOptions{MaxDepth: 3})
	require.NoError(t, err)

	fwd := moduleNames(forward)
	assert.Equal(t, "1.0.0", fwd["p"], "p stays at 1.0.0 across the depth relabel")
	assert.Equal(t, "1.0.0", fwd["sp"], "sp narrows to 1.0.0, retracting p's shallow source")
	assert.NotContains(t, fwd, "c", "c must be relabeled to the deeper depth and rejected")
	assert.True(t, hasDepthError(forward, "c"), "c must fail loud for exceeding max depth")

	reverse, err := Resolve(context.Background(), build(),
		[]DependencySpec{rootNarrow, rootDeep, rootSP}, &ResolveOptions{MaxDepth: 3})
	require.NoError(t, err)

	assert.Equal(t, fwd, moduleNames(reverse), "resolved set must not depend on root order")
	assert.Equal(t, hasDepthError(forward, "c"), hasDepthError(reverse, "c"),
		"depth outcome must not depend on root order")
}

func TestManifestCache_LRUBoundedCapacity(t *testing.T) {
	p := newFakeProvider()
	for i := 0; i < 5; i++ {
		p.addModule("acme", "m"+fmt.Sprint(i), "1.0.0")
	}

	cache := NewManifestCacheWithOptions(p, 2, 0, 0)
	defer cache.Close()

	for i := 0; i < 5; i++ {
		_, err := cache.GetManifest(context.Background(), "acme", "m"+fmt.Sprint(i), "1.0.0")
		require.NoError(t, err)
	}

	assert.LessOrEqual(t, cache.Len(), 2,
		"LRU must enforce capacity even under churn")
}

func TestManifestCache_RefreshOverwritesStale(t *testing.T) {
	p := newMutableProvider()
	p.addModule("acme", "http", "1.0.0")

	cache := NewManifestCache(p)
	defer cache.Close()
	_ = cache.store.Set("acme/http@1.0.0", &ModuleManifest{
		Org: "acme", Name: "http", Version: "1.0.0", Digest: "sha256:stale",
	})

	hit, _ := cache.GetManifest(context.Background(), "acme", "http", "1.0.0")
	require.NotNil(t, hit)
	assert.Equal(t, "sha256:stale", hit.Digest)

	fresh, err := cache.Refresh(context.Background(), "acme", "http", "1.0.0")
	require.NoError(t, err)
	require.NotNil(t, fresh)
	assert.Equal(t, "sha256:1.0.0", fresh.Digest, "Refresh must bypass cache and store fresh")

	after, _ := cache.GetManifest(context.Background(), "acme", "http", "1.0.0")
	require.NotNil(t, after)
	assert.Equal(t, "sha256:1.0.0", after.Digest, "subsequent Get must see refreshed manifest")
}

func TestManifestCache_MutableLabelAlwaysRefreshes(t *testing.T) {
	p := newMutableProvider()
	p.override("acme", "http", "@latest", "sha256:first")
	cache := NewManifestCache(p)
	defer cache.Close()

	first, err := cache.GetManifest(context.Background(), "acme", "http", "@latest")
	require.NoError(t, err)
	require.Equal(t, "sha256:first", first.Digest)

	p.override("acme", "http", "@latest", "sha256:second")
	second, err := cache.GetManifest(context.Background(), "acme", "http", "@latest")
	require.NoError(t, err)
	require.Equal(t, "sha256:second", second.Digest)
}

func TestResolve_RejectsManifestIdentityMismatch(t *testing.T) {
	p := newMutableProvider()
	p.overrides["acme/http@1.0.0"] = &ModuleManifest{
		Org: "other", Name: "module", Version: "1.0.0",
	}

	result, err := Resolve(context.Background(), p, []DependencySpec{{
		Org: "acme", Name: "http", Constraint: "1.0.0",
	}}, nil)
	require.NoError(t, err)
	require.Empty(t, result.Modules)
	require.Len(t, result.Errors, 1)
	require.Contains(t, result.Errors[0].Message, "manifest identity mismatch")
}

func TestManifestCache_TTLExpiry(t *testing.T) {
	p := newFakeProvider()
	p.addModule("acme", "http", "1.0.0")

	cache := NewManifestCacheWithOptions(p, 16, 25*time.Millisecond, 0)
	defer cache.Close()

	_, err := cache.GetManifest(context.Background(), "acme", "http", "1.0.0")
	require.NoError(t, err)
	require.Equal(t, 1, cache.Len())

	time.Sleep(50 * time.Millisecond)

	_, hit := cache.store.Get("acme/http@1.0.0")
	assert.False(t, hit, "expired entry must not be served after TTL")
}

// dataflowProvider mirrors the real keeper/dataflow graph: keeper declares
// ">=v0.4.10" for dataflow, and the hub resolved that range to 0.5.2 at request
// time. dataflow ships 0.4.x releases and a breaking 0.5.x line.
func dataflowProvider(constraint string) *fakeManifestProvider {
	p := newFakeProvider()
	p.addModule("keeper", "keeper", "0.5.57", ManifestDep{
		Org:        "wippy",
		Name:       "dataflow",
		Version:    "0.5.2",
		Constraint: constraint,
	})
	for _, v := range []string{"0.4.10", "0.4.31", "0.5.0", "0.5.2"} {
		p.addModule("wippy", "dataflow", v)
	}
	return p
}

func resolveKeeper(t *testing.T, p *fakeManifestProvider, opts *ResolveOptions) map[string]string {
	t.Helper()

	result, err := Resolve(context.Background(), p, []DependencySpec{
		{Org: "keeper", Name: "keeper", Constraint: "0.5.57"},
	}, opts)
	require.NoError(t, err)
	assert.Empty(t, result.Errors)

	got := make(map[string]string, len(result.Modules))
	for _, m := range result.Modules {
		got[m.Org+"/"+m.Name] = m.Version
	}
	return got
}

// A lock pinning a version that satisfies the declared range must survive. The
// hub resolving ">=v0.4.10" to 0.5.2 is not a demand for 0.5.2; treating it as
// one discards the lock and force-bumps the install to latest.
func TestResolve_LockedVersionSurvivesDeclaredRange(t *testing.T) {
	got := resolveKeeper(t, dataflowProvider(">=v0.4.10"), &ResolveOptions{
		LockedVersions: map[string]string{"wippy/dataflow": "0.4.31"},
	})

	assert.Equal(t, "0.4.31", got["wippy/dataflow"])
}

// Without a lock the range still selects the newest matching release: preserving
// the constraint must not change what ">=" means.
func TestResolve_UnlockedDeclaredRangeTakesNewest(t *testing.T) {
	got := resolveKeeper(t, dataflowProvider(">=v0.4.10"), nil)

	assert.Equal(t, "0.5.2", got["wippy/dataflow"])
}

// A hub predating the constraint field sends only the resolved version, so the
// resolver has nothing but that pin to go on. Asserted against a lock the pin
// does not satisfy: the lock must lose, which is what distinguishes this path
// from a real range (where the lock would win). Without the lock the assertion
// would hold either way and prove nothing.
func TestResolve_MissingConstraintFallsBackToResolvedVersion(t *testing.T) {
	got := resolveKeeper(t, dataflowProvider(""), &ResolveOptions{
		LockedVersions: map[string]string{"wippy/dataflow": "0.4.31"},
	})

	assert.Equal(t, "0.5.2", got["wippy/dataflow"])
}

// An unconstrained dependency accepts any version, so a locked one still holds.
// Hub reports it as "*"; treating that as a pin would bump it to latest.
func TestResolve_LockedVersionSurvivesAnyConstraint(t *testing.T) {
	got := resolveKeeper(t, dataflowProvider("*"), &ResolveOptions{
		LockedVersions: map[string]string{"wippy/dataflow": "0.4.31"},
	})

	assert.Equal(t, "0.4.31", got["wippy/dataflow"])
}

// Many modules depending on one module, each declaring its own compatible
// two-sided range, is an ordinary graph. Preserving the declared ranges makes
// those constraints distinct, so the module now resolves through the
// intersection path -- which must still resolve, not report a conflict.
func TestResolve_ManyParentsWithCompatibleRanges(t *testing.T) {
	p := newFakeProvider()

	// Distinct ranges: each parent pins its own floor, all mutually compatible.
	parents := map[string]string{
		"alpha":   ">=v0.4.10 <v0.5.0",
		"bravo":   ">=v0.4.11 <v0.5.0",
		"charlie": ">=v0.4.12 <v0.5.0",
		"delta":   ">=v0.4.13 <v0.5.0",
		"echo":    ">=v0.4.14 <v0.5.0",
		"foxtrot": ">=v0.4.15 <v0.5.0",
	}
	roots := make([]DependencySpec, 0, len(parents))
	for parent, constraint := range parents {
		p.addModule("acme", parent, "1.0.0", ManifestDep{
			Org:        "wippy",
			Name:       "dataflow",
			Version:    "0.4.31",
			Constraint: constraint,
		})
		roots = append(roots, DependencySpec{Org: "acme", Name: parent, Constraint: "1.0.0"})
	}
	for _, v := range []string{"0.4.10", "0.4.31", "0.5.0"} {
		p.addModule("wippy", "dataflow", v)
	}

	result, err := Resolve(context.Background(), p, roots, nil)
	require.NoError(t, err)

	assert.Empty(t, result.Errors, "compatible ranges must not be reported as a conflict")

	for _, m := range result.Modules {
		if m.Org == "wippy" && m.Name == "dataflow" {
			assert.Equal(t, "0.4.31", m.Version)
			return
		}
	}
	t.Fatal("wippy/dataflow was not resolved at all")
}

// A valid but unsatisfied range set is an availability failure, not a parser
// conflict. Publishing a compatible release may make it resolvable.
func TestResolve_IncompatibleRangesReportAvailability(t *testing.T) {
	p := newFakeProvider()
	p.addModule("acme", "alpha", "1.0.0", ManifestDep{
		Org: "wippy", Name: "dataflow", Version: "0.4.31", Constraint: ">=v0.4.10 <v0.5.0",
	})
	p.addModule("acme", "bravo", "1.0.0", ManifestDep{
		Org: "wippy", Name: "dataflow", Version: "0.5.0", Constraint: ">=v0.5.0",
	})
	for _, v := range []string{"0.4.31", "0.5.0"} {
		p.addModule("wippy", "dataflow", v)
	}

	result, err := Resolve(context.Background(), p, []DependencySpec{
		{Org: "acme", Name: "alpha", Constraint: "1.0.0"},
		{Org: "acme", Name: "bravo", Constraint: "1.0.0"},
	}, nil)
	require.NoError(t, err)

	require.NotEmpty(t, result.Errors, "no version satisfies both ranges")
	assert.Contains(t, result.Errors[0].Message, "no available version of wippy/dataflow")
	assert.NotContains(t, result.Errors[0].Message, "conflicting version constraints")
}
