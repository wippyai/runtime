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
