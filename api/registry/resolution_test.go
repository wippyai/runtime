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
