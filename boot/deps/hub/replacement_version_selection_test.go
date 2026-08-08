// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/boot/deps/lock"
	"go.uber.org/zap"
)

const replacementCRMSpec = "spiralscout/crm"

func newReplacementProvider(t *testing.T, version string, base ManifestProvider, locked string) *replacementManifestProvider {
	t.Helper()

	path := t.TempDir()
	if version != "" {
		require.NoError(t, os.WriteFile(filepath.Join(path, "wippy.yaml"), []byte("version: "+version+"\n"), 0o600))
	}

	return &replacementManifestProvider{
		base: base,
		handler: &DependencyHandler{
			logger: zap.NewNop(),
			replacements: map[string]lock.Replacement{
				replacementCRMSpec: {From: replacementCRMSpec, To: path},
			},
		},
		lockedVersions: map[string]string{replacementCRMSpec: locked},
	}
}

func crmReplacementRoots(constraints ...string) []DependencySpec {
	roots := make([]DependencySpec, 0, len(constraints))
	for _, constraint := range constraints {
		roots = append(roots, DependencySpec{Org: "spiralscout", Name: "crm", Constraint: constraint})
	}
	return roots
}

func TestReplacementManifestProvider_UnversionedSourceUsesHubCandidatesWhenLockDoesNotSatisfy(t *testing.T) {
	base := newFakeProvider()
	base.addModule("spiralscout", "crm", "0.1.44")
	base.addModule("spiralscout", "crm", "0.1.49")
	provider := newReplacementProvider(t, "", base, "0.1.44")

	result, err := Resolve(newTestContext(), provider, crmReplacementRoots("*", ">=0.1.17", ">=0.1.49"), &ResolveOptions{
		LockedVersions: map[string]string{replacementCRMSpec: "0.1.44"},
	})
	require.NoError(t, err)
	require.Empty(t, result.Errors)
	require.Len(t, result.Modules, 1)
	assert.Equal(t, "0.1.49", result.Modules[0].Version)
	assert.Equal(t, 1, base.listAllVersion[replacementCRMSpec])
	assert.Zero(t, base.getManifest[replacementCRMSpec], "the local source supplies the resolved manifest")
}

func TestReplacementManifestProvider_LockRemainsAuthoritativeWhenItSatisfies(t *testing.T) {
	base := newFakeProvider()
	base.addModule("spiralscout", "crm", "0.1.44")
	base.addModule("spiralscout", "crm", "0.1.49")
	provider := newReplacementProvider(t, "", base, "0.1.44")

	result, err := Resolve(newTestContext(), provider, crmReplacementRoots("*", ">=0.1.17"), &ResolveOptions{
		LockedVersions: map[string]string{replacementCRMSpec: "0.1.44"},
	})
	require.NoError(t, err)
	require.Empty(t, result.Errors)
	require.Len(t, result.Modules, 1)
	assert.Equal(t, "0.1.44", result.Modules[0].Version)
	assert.Zero(t, base.listAllVersion[replacementCRMSpec], "a satisfying lock must not resolve through the Hub")
	assert.Zero(t, base.getManifest[replacementCRMSpec])
}

func TestReplacementManifestProvider_ExplicitSourceVersionDoesNotDelegateCandidates(t *testing.T) {
	base := newFakeProvider()
	base.addModule("spiralscout", "crm", "0.1.44")
	base.addModule("spiralscout", "crm", "0.1.49")
	provider := newReplacementProvider(t, "0.1.44", base, "0.1.44")

	result, err := Resolve(newTestContext(), provider, crmReplacementRoots(">=0.1.49"), &ResolveOptions{
		LockedVersions: map[string]string{replacementCRMSpec: "0.1.44"},
	})
	require.NoError(t, err)
	require.Empty(t, result.Modules)
	require.Len(t, result.Errors, 1)
	assert.Contains(t, result.Errors[0].Message, "no available version of spiralscout/crm")
	assert.NotContains(t, result.Errors[0].Message, "conflicting version constraints")
	assert.Zero(t, base.listAllVersion[replacementCRMSpec], "an explicit local version is authoritative")
	assert.Zero(t, base.getManifest[replacementCRMSpec])
}

func TestReplacementManifestProvider_UnversionedSourceUsesHubCandidatesForSelection(t *testing.T) {
	base := newFakeProvider()
	base.addModule("spiralscout", "crm", "0.1.44")
	base.addModule("spiralscout", "crm", "0.1.48")
	provider := newReplacementProvider(t, "", base, "0.1.44")

	result, err := Resolve(context.Background(), provider, crmReplacementRoots(">=0.1.17", ">=0.1.49"), &ResolveOptions{
		LockedVersions: map[string]string{replacementCRMSpec: "0.1.44"},
	})
	require.NoError(t, err)
	require.Empty(t, result.Modules)
	require.Len(t, result.Errors, 1)
	assert.Contains(t, result.Errors[0].Message, "no available version of spiralscout/crm")
	assert.Contains(t, result.Errors[0].Message, ">=0.1.17")
	assert.Contains(t, result.Errors[0].Message, ">=0.1.49")
	assert.NotContains(t, result.Errors[0].Message, "conflicting version constraints")
}
