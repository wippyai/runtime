// SPDX-License-Identifier: MPL-2.0

package component

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/registry"
	wasmrt "github.com/wippyai/wasm-runtime/runtime"
)

func TestHostRegistry_DeprecationMetadataInspection(t *testing.T) {
	r := NewHostRegistry()

	depInfo := DeprecationInfo{
		Deprecated:           true,
		Replacement:          "socket",
		MinimumVersions:      10,
		FirstReleasedVersion: "", // unassigned for draft
		Notes:                "legacy socket binary namespace wippy:sock/tcp and YAML alias wippy:sock are deprecated; supported for minimum 10 runtime versions",
	}

	require.NoError(t, r.RegisterProfiles(HostProfile{
		Name: HostProfileSocket,
		Aliases: []string{
			"wippy:runtime/socket@0.1.0",
			"wippy:sock",
			"wippy:sock/tcp",
		},
		DeprecatedAliases: map[string]DeprecationInfo{
			"wippy:sock":     depInfo,
			"wippy:sock/tcp": depInfo,
		},
	}))

	// Inspect via Deprecation(id)
	dep, ok := r.Deprecation(registry.ParseID("wippy:sock"))
	require.True(t, ok)
	assert.True(t, dep.Deprecated)
	assert.Equal(t, "socket", dep.Replacement)
	assert.Equal(t, 10, dep.MinimumVersions)
	assert.Empty(t, dep.FirstReleasedVersion)
	assert.NotEmpty(t, dep.Notes)

	depTCP, okTCP := r.Deprecation(registry.ParseID("wippy:sock/tcp"))
	require.True(t, okTCP)
	assert.True(t, depTCP.Deprecated)
	assert.Equal(t, "socket", depTCP.Replacement)

	// Canonical profile and binary namespace must NOT be marked deprecated
	_, okCanonical := r.Deprecation(registry.ParseID("socket"))
	assert.False(t, okCanonical, "canonical profile socket must not be deprecated")

	_, okCanonicalBinary := r.Deprecation(registry.ParseID("wippy:runtime/socket@0.1.0"))
	assert.False(t, okCanonicalBinary, "canonical binary namespace must not be deprecated")

	// Direct alias inspection
	depAlias, okAlias := r.DeprecationForAlias("wippy:sock")
	require.True(t, okAlias)
	assert.Equal(t, 10, depAlias.MinimumVersions)

	_, okUnknown := r.DeprecationForAlias("unknown")
	assert.False(t, okUnknown)
}

func TestHostRegistry_ResolvesOldAndNewAliasesToCanonicalProfile(t *testing.T) {
	r := NewHostRegistry()

	require.NoError(t, r.RegisterProfiles(HostProfile{
		Name: HostProfileSocket,
		Aliases: []string{
			"wippy:runtime/socket@0.1.0",
			"wippy:sock",
			"wippy:sock/tcp",
		},
	}))

	testCases := []struct {
		name string
		raw  string
	}{
		{name: "canonical short", raw: "socket"},
		{name: "canonical versioned binary", raw: "wippy:runtime/socket@0.1.0"},
		{name: "legacy yaml alias", raw: "wippy:sock"},
		{name: "legacy binary namespace", raw: "wippy:sock/tcp"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			profile, ok := r.Resolve(registry.ParseID(tc.raw))
			require.True(t, ok)
			assert.Equal(t, HostProfileSocket, profile.Name)
		})
	}
}

func TestHostRegistry_DeduplicatedWarningPerRuntimeAndAlias(t *testing.T) {
	r := NewHostRegistry()

	var warningCalls atomic.Int32
	var lastAlias atomic.Value

	depInfo := DeprecationInfo{
		Deprecated:           true,
		Replacement:          "socket",
		MinimumVersions:      10,
		FirstReleasedVersion: "",
	}

	var registerCalls atomic.Int32
	require.NoError(t, r.RegisterProfiles(HostProfile{
		Name: HostProfileSocket,
		Aliases: []string{
			"wippy:runtime/socket@0.1.0",
			"wippy:sock",
			"wippy:sock/tcp",
		},
		DeprecatedAliases: map[string]DeprecationInfo{
			"wippy:sock":     depInfo,
			"wippy:sock/tcp": depInfo,
		},
		DeprecationCallback: func(_ context.Context, alias string, _ DeprecationInfo) {
			warningCalls.Add(1)
			lastAlias.Store(alias)
		},
		Register: func(context.Context, *wasmrt.Runtime) error {
			registerCalls.Add(1)
			return nil
		},
	}))

	ctx := context.Background()
	rt1 := &wasmrt.Runtime{}
	rt2 := &wasmrt.Runtime{}

	// 1. First EnsureImports with legacy YAML alias on rt1 -> fires warning callback once
	err := r.EnsureImports(ctx, rt1, []registry.ID{registry.ParseID("wippy:sock")}, false)
	require.NoError(t, err)
	assert.Equal(t, int32(1), warningCalls.Load())
	assert.Equal(t, "wippy:sock", lastAlias.Load())
	assert.Equal(t, int32(1), registerCalls.Load(), "profile must register once")

	// 2. Second EnsureImports with same legacy alias on rt1 -> deduplicated, no additional warning
	err = r.EnsureImports(ctx, rt1, []registry.ID{registry.ParseID("wippy:sock")}, false)
	require.NoError(t, err)
	assert.Equal(t, int32(1), warningCalls.Load(), "warning must be deduplicated per runtime/alias")
	assert.Equal(t, int32(1), registerCalls.Load(), "profile already loaded on rt1")

	// 3. EnsureImports with canonical name on rt1 -> zero warnings emitted
	err = r.EnsureImports(ctx, rt1, []registry.ID{registry.ParseID("socket")}, false)
	require.NoError(t, err)
	assert.Equal(t, int32(1), warningCalls.Load(), "canonical import must produce no warning")

	// 4. EnsureImports on independent runtime rt2 -> warning fires once for rt2
	err = r.EnsureImports(ctx, rt2, []registry.ID{registry.ParseID("wippy:sock")}, false)
	require.NoError(t, err)
	assert.Equal(t, int32(2), warningCalls.Load(), "independent runtime must receive warning once")
	assert.Equal(t, int32(2), registerCalls.Load(), "profile registers once for rt2")

	// 5. ResetLoaded clears state; subsequent EnsureImports on rt1 warns again without leaks
	r.ResetLoaded()
	err = r.EnsureImports(ctx, rt1, []registry.ID{registry.ParseID("wippy:sock")}, false)
	require.NoError(t, err)
	assert.Equal(t, int32(3), warningCalls.Load(), "after ResetLoaded, warning triggers once for new lifecycle")
}

func TestHostRegistry_ForkPreservesDeprecations(t *testing.T) {
	parent := NewHostRegistry()

	depInfo := DeprecationInfo{
		Deprecated:      true,
		Replacement:     "socket",
		MinimumVersions: 10,
	}

	require.NoError(t, parent.RegisterProfiles(HostProfile{
		Name: HostProfileSocket,
		Aliases: []string{
			"wippy:runtime/socket@0.1.0",
			"wippy:sock",
		},
		DeprecatedAliases: map[string]DeprecationInfo{
			"wippy:sock": depInfo,
		},
	}))

	child := parent.Fork()

	dep, ok := child.Deprecation(registry.ParseID("wippy:sock"))
	require.True(t, ok)
	assert.True(t, dep.Deprecated)
	assert.Equal(t, 10, dep.MinimumVersions)

	profile, ok := child.Resolve(registry.ParseID("wippy:sock"))
	require.True(t, ok)
	assert.Equal(t, HostProfileSocket, profile.Name)
}
