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

	require.NotEqual(t, base.Digest, changedInput.Digest)
	require.NotEqual(t, base.Digest, changedArtifact.Digest)
}
