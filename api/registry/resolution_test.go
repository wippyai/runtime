// SPDX-License-Identifier: MPL-2.0

package registry

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDependencyResolutionCanonicalDigestIsOrderIndependent(t *testing.T) {
	left := (&DependencyResolution{
		InputDigest: "roots",
		Modules: []ResolvedModule{
			{Name: "acme/z", Version: "v2", Digest: "sha256:z"},
			{Name: "acme/a", Version: "v1", Digest: "sha256:a"},
		},
	}).Canonical()
	right := (&DependencyResolution{
		InputDigest: "roots",
		Modules: []ResolvedModule{
			{Name: "acme/a", Version: "v1", Digest: "sha256:a"},
			{Name: "acme/z", Version: "v2", Digest: "sha256:z"},
		},
	}).Canonical()

	require.Equal(t, left.Digest, right.Digest)
	require.True(t, left.Valid())
	require.True(t, right.Valid())
}

func TestDependencyResolutionDigestCoversInputsAndArtifacts(t *testing.T) {
	base := (&DependencyResolution{
		InputDigest: "roots-a",
		Modules:     []ResolvedModule{{Name: "acme/a", Version: "v1", Digest: "sha256:a"}},
	}).Canonical()
	changedInput := (&DependencyResolution{
		InputDigest: "roots-b",
		Modules:     base.Modules,
	}).Canonical()
	changedArtifact := (&DependencyResolution{
		InputDigest: "roots-a",
		Modules:     []ResolvedModule{{Name: "acme/a", Version: "v1", Digest: "sha256:b"}},
	}).Canonical()
	changedSource := (&DependencyResolution{
		InputDigest: "roots-a",
		Modules:     []ResolvedModule{{Name: "acme/a", Version: "v1", Source: "replacement-tree-v1", Digest: "sha256:a"}},
	}).Canonical()

	require.NotEqual(t, base.Digest, changedInput.Digest)
	require.NotEqual(t, base.Digest, changedArtifact.Digest)
	require.NotEqual(t, base.Digest, changedSource.Digest)
}

func TestDependencyResolutionValidRejectsSemanticDuplicates(t *testing.T) {
	duplicate := (&DependencyResolution{
		InputDigest: "roots",
		Modules: []ResolvedModule{
			{Name: "acme/a", Version: "v1"},
			{Name: "acme/a", Version: "v2"},
		},
	}).Canonical()
	require.False(t, duplicate.Valid())
}

func TestDependencyResolutionValidRequiresArtifactIdentity(t *testing.T) {
	missingDigest := (&DependencyResolution{
		InputDigest: "roots",
		Modules:     []ResolvedModule{{Name: "acme/a", Version: "v1"}},
	}).Canonical()
	require.False(t, missingDigest.Valid())
}

func TestCanRebaseDependencyResolutionRequiresBaselineTransition(t *testing.T) {
	graph := func(baseline, version string) *DependencyResolution {
		return (&DependencyResolution{
			BaselineDigest: baseline,
			InputDigest:    "sha256:inputs",
			Roots:          []DependencyRoot{{ID: "app.deps:app", Component: "acme/app", Version: ">=v1.0.0"}},
			Modules:        []ResolvedModule{{Name: "acme/app", Version: version, Digest: "sha256:artifact" + version}},
		}).Canonical()
	}

	existing := graph("sha256:baseline-a", "v1.0.0")
	rebased := graph("sha256:baseline-b", "v2.0.0")
	rewritten := graph("sha256:baseline-a", "v2.0.0")
	legacy := graph("", "v1.0.0")

	if !CanRebaseDependencyResolution(existing, rebased) {
		t.Fatal("a changed deployment baseline must permit graph rebinding")
	}
	if !CanRebaseDependencyResolution(legacy, rebased) {
		t.Fatal("a legacy unbound graph must be upgradeable")
	}
	if CanRebaseDependencyResolution(existing, rewritten) {
		t.Fatal("the graph must remain immutable within one deployment baseline")
	}
	if CanRebaseDependencyResolution(existing, existing) {
		t.Fatal("an idempotent checkpoint is not a rebase")
	}
}

func TestCanRebaseDependencyResolutionPermitsDeploymentBindingOnce(t *testing.T) {
	existing := (&DependencyResolution{
		BaselineDigest: "sha256:baseline",
		InputDigest:    "sha256:inputs",
		Roots:          []DependencyRoot{{ID: "app.deps:app", Component: "acme/app", Version: "1.0.0"}},
		Modules:        []ResolvedModule{{Name: "acme/app", Version: "1.0.0", Digest: "sha256:app"}},
	}).Canonical()
	next := existing.Canonical()
	next.Deployment = &Deployment{
		Root:    "acme/app",
		Modules: []ResolvedModule{{Name: "acme/app", Version: "1.0.0", Digest: "sha256:app"}},
	}
	next = next.Canonical()

	require.True(t, next.Valid())
	require.True(t, CanRebaseDependencyResolution(existing, next))

	changed := next.Canonical()
	changed.Deployment.Modules[0].Version = "2.0.0"
	changed = changed.Canonical()
	require.False(t, CanRebaseDependencyResolution(next, changed))
}

func TestDependencyResolutionDeploymentIsCanonicalAndValidated(t *testing.T) {
	resolution := (&DependencyResolution{
		InputDigest: "sha256:inputs",
		Deployment: &Deployment{
			Root: "acme/app",
			Modules: []ResolvedModule{
				{Name: "acme/worker", Version: "1.0.0", Digest: "sha256:worker"},
				{Name: "acme/app", Version: "1.0.0", Digest: "sha256:app"},
			},
		},
	}).Canonical()

	require.True(t, resolution.Valid())
	require.Equal(t, "acme/app", resolution.Deployment.Modules[0].Name)

	missingRoot := resolution.Canonical()
	missingRoot.Deployment.Root = "acme/missing"
	missingRoot = missingRoot.Canonical()
	require.False(t, missingRoot.Valid())
}

func TestDependencyResolutionReferencesStayOutsideDigests(t *testing.T) {
	base := &DependencyResolution{
		InputDigest: "sha256:input",
		Roots: []DependencyRoot{
			{ID: "app.deps:tools", Component: "acme/tools", Version: ">=0.1.0"},
		},
		Modules: []ResolvedModule{
			{Name: "acme/tools", Version: "0.2.5", Digest: "sha256:mod"},
		},
	}
	withReferences := &DependencyResolution{
		InputDigest: base.InputDigest,
		Roots:       append([]DependencyRoot(nil), base.Roots...),
		References: []DependencyRoot{
			{ID: "acme.pkg:__dependency.acme.tools", Component: "acme/tools", Version: ">=0.1.0"},
		},
		Modules: append([]ResolvedModule(nil), base.Modules...),
	}

	// References are digest-relevant — a referenced graph must not collide with
	// its reference-free shape in content-addressed stores — while an EMPTY
	// reference set keeps the digest byte-identical to prior releases.
	if base.Canonical().Digest == withReferences.Canonical().Digest {
		t.Fatalf("referenced graph must be content-distinct from the reference-free graph")
	}
	emptyRefs := &DependencyResolution{
		InputDigest: base.InputDigest,
		Roots:       append([]DependencyRoot(nil), base.Roots...),
		References:  []DependencyRoot{},
		Modules:     append([]ResolvedModule(nil), base.Modules...),
	}
	if base.Canonical().Digest != emptyRefs.Canonical().Digest {
		t.Fatalf("empty reference set must keep the legacy digest")
	}

	canonical := withReferences.Canonical()
	if len(canonical.References) != 1 || canonical.References[0].ID != "acme.pkg:__dependency.acme.tools" {
		t.Fatalf("canonical resolution must retain references: %+v", canonical.References)
	}
	if !canonical.Valid() {
		t.Fatalf("referenced resolution must validate")
	}

	// References must anchor to a root component and never duplicate root IDs.
	broken := withReferences.Canonical()
	broken.References[0].Component = "acme/other"
	broken.Digest = broken.computeDigest()
	if broken.Valid() {
		t.Fatalf("unanchored reference must invalidate the resolution")
	}
	colliding := withReferences.Canonical()
	colliding.References[0].ID = "app.deps:tools"
	colliding.Digest = colliding.computeDigest()
	if colliding.Valid() {
		t.Fatalf("reference colliding with a root ID must invalidate the resolution")
	}
}

func TestCanRebaseDependencyResolutionIgnoresReferenceOnlyChanges(t *testing.T) {
	base := (&DependencyResolution{
		InputDigest:    "sha256:input",
		BaselineDigest: "sha256:baseline",
		Roots: []DependencyRoot{
			{ID: "app.deps:tools", Component: "acme/tools", Version: ">=0.1.0"},
		},
		Modules: []ResolvedModule{
			{Name: "acme/tools", Version: "0.2.5", Digest: "sha256:mod"},
		},
	}).Canonical()
	referenced := (&DependencyResolution{
		InputDigest:    base.InputDigest,
		BaselineDigest: base.BaselineDigest,
		Roots:          append([]DependencyRoot(nil), base.Roots...),
		References: []DependencyRoot{
			{ID: "acme.pkg:__dependency.acme.tools", Component: "acme/tools", Version: ">=0.1.0"},
		},
		Modules: append([]ResolvedModule(nil), base.Modules...),
	}).Canonical()

	// A reference-only change shares the deployment baseline: it is a normal
	// new registry version, never a rebind of an existing one.
	if CanRebaseDependencyResolution(base, referenced) {
		t.Fatalf("reference-only change within one baseline must not permit a rebind")
	}

	// A baseline transition keeps permitting the one-shot rebind even when the
	// next graph carries references.
	rebased := referenced.Canonical()
	rebased.BaselineDigest = "sha256:next-baseline"
	rebased = rebased.Canonical()
	if !CanRebaseDependencyResolution(base, rebased) {
		t.Fatalf("baseline transition with references must permit the rebind")
	}
}

func TestDependencyResolutionValidRequiresModuleForEveryRoot(t *testing.T) {
	orphanRoot := (&DependencyResolution{
		InputDigest: "sha256:input",
		Roots: []DependencyRoot{
			{ID: "app.deps:tools", Component: "acme/tools", Version: ">=0.1.0"},
		},
		Modules: []ResolvedModule{
			{Name: "acme/other", Version: "1.0.0", Digest: "sha256:mod"},
		},
	}).Canonical()
	require.False(t, orphanRoot.Valid(), "a root whose component has no selected module is not a resolution of its own declarations")
}

func TestDependencyResolutionValidRejectsProvablyUnsatisfiedConstraints(t *testing.T) {
	unsoundReference := (&DependencyResolution{
		InputDigest: "sha256:input",
		Roots: []DependencyRoot{
			{ID: "app.deps:tools", Component: "acme/tools", Version: ">=1.0.0"},
		},
		References: []DependencyRoot{
			{ID: "acme.pkg:__dependency.acme.tools", Component: "acme/tools", Version: ">=2.0.0"},
		},
		Modules: []ResolvedModule{
			{Name: "acme/tools", Version: "1.0.0", Digest: "sha256:mod"},
		},
	}).Canonical()
	require.False(t, unsoundReference.Valid(), "a reference constraint the selected version cannot satisfy must not be storable")

	unsoundRoot := (&DependencyResolution{
		InputDigest: "sha256:input",
		Roots: []DependencyRoot{
			{ID: "app.deps:tools", Component: "acme/tools", Version: ">=2.0.0"},
		},
		Modules: []ResolvedModule{
			{Name: "acme/tools", Version: "1.0.0", Digest: "sha256:mod"},
		},
	}).Canonical()
	require.False(t, unsoundRoot.Valid(), "a root constraint the selected version cannot satisfy must not be storable")
}

func TestDependencyResolutionValidBindsOnlyOnParseableSemverSpellings(t *testing.T) {
	graph := func(constraint, selected string) *DependencyResolution {
		return (&DependencyResolution{
			InputDigest: "sha256:input",
			Roots: []DependencyRoot{
				{ID: "app.deps:tools", Component: "acme/tools", Version: constraint},
			},
			Modules: []ResolvedModule{
				{Name: "acme/tools", Version: selected, Digest: "sha256:mod"},
			},
		}).Canonical()
	}

	// Channel pins, branch selections, and exact literals carry hub semantics
	// the model cannot interpret; validity must not bind on them.
	require.True(t, graph("@beta", "1.0.0-beta.2").Valid())
	require.True(t, graph("main", "main").Valid())
	require.True(t, graph("2.0.0", "1.0.0").Valid())
	// A semver constraint over an uninterpretable selected version stays open.
	require.True(t, graph(">=1.0.0", "branch-build").Valid())
	// Wildcards are constraints and always satisfied.
	require.True(t, graph("*", "0.0.1").Valid())
	require.True(t, graph("1.x", "1.4.0").Valid())
	require.False(t, graph("1.x", "2.0.0").Valid())
}

// The digest below was produced by the code preceding the reference fold
// (7d61cc743) over this exact fixture. A reference-free graph must keep it
// byte-for-byte: existing applications' history rows and content-addressed
// graphs depend on the recipe never shifting for the empty-reference shape.
func TestDependencyResolutionGoldenPreChangeDigest(t *testing.T) {
	graph := (&DependencyResolution{
		InputDigest:    "sha256:golden-input",
		BaselineDigest: "sha256:golden-baseline",
		Roots: []DependencyRoot{
			{ID: "app.deps:tools", Component: "acme/tools", Version: ">=0.1.0"},
			{ID: "app.deps:app", Component: "acme/app", Version: "v1.0.0"},
		},
		Modules: []ResolvedModule{
			{Name: "acme/tools", Version: "0.2.5", VersionID: "vid-tools", Source: "hub", Digest: "sha256:tools", SizeBytes: 42},
			{Name: "acme/app", Version: "v1.0.0", Digest: "sha256:app", Protected: true},
		},
	}).Canonical()
	require.Equal(t, "sha256:baccc9647874aef7aa046c781fc4eb087e2ad7c65e4bb10f3b41801c8a5ab4bc", graph.Digest)
	require.True(t, graph.Valid())
}
