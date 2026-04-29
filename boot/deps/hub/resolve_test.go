// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeManifestProvider struct {
	manifests map[string]*ModuleManifest
	versions  map[string][]VersionInfo
}

func newFakeProvider() *fakeManifestProvider {
	return &fakeManifestProvider{
		manifests: make(map[string]*ModuleManifest),
		versions:  make(map[string][]VersionInfo),
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
	key := org + "/" + module + "@" + constraint
	if m, ok := f.manifests[key]; ok {
		return m, nil
	}
	return nil, fmt.Errorf("module %s/%s@%s not found", org, module, constraint)
}

func (f *fakeManifestProvider) ListAllVersions(_ context.Context, org, module string) ([]VersionInfo, error) {
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
	assert.Contains(t, result.Errors[0].Message, "no version matching")
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
