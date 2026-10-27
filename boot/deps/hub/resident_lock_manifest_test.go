// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/boot/deps/lock"
)

// A lock-booted instance records no deployment. Its lock pins each module by
// digest, so a resident module whose recorded digest equals the lock pin is
// known content even when the configured registry does not hold that release.
func TestLockBootedResidentManifestsForLockPinnedModules(t *testing.T) {
	const otherDigest = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	for _, tc := range []struct {
		name       string
		lockHash   string
		hubDigest  string
		wantError  string
		wantDigest string
		wantHub    bool
	}{
		{name: "registry lacks the locked release", lockHash: lockedResolutionDigest, wantHub: true,
			wantDigest: "sha256:" + lockedResolutionDigest},
		{name: "registry serves the locked digest", lockHash: lockedResolutionDigest, hubDigest: "sha256:" + lockedResolutionDigest,
			wantHub: true, wantDigest: "sha256:" + lockedResolutionDigest},
		{name: "registry serves another digest", lockHash: lockedResolutionDigest, hubDigest: "sha256:" + otherDigest,
			wantHub: true, wantError: "manifest digest mismatch"},
		{name: "recorded digest differs from the lock", lockHash: otherDigest,
			wantError: "module not found"},
		{name: "lock has no digest", wantError: "module not found"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			selected := regapi.ResolvedModule{Name: "acme/resident", Version: "1.0.0", Source: moduleSourceHub,
				Digest: "sha256:" + lockedResolutionDigest}
			resolution := (&regapi.DependencyResolution{
				Roots:   []regapi.DependencyRoot{{ID: "host.deps:resident", Component: selected.Name, Version: "*"}},
				Modules: []regapi.ResolvedModule{selected},
			}).Canonical()
			require.True(t, resolution.Valid())
			state := regapi.State{{ID: regapi.NewID("acme.resident", "definition"), Kind: regapi.NamespaceDefinition,
				Registry: regapi.EntryMetadata{Owner: selected.Name}, Data: payload.New(map[string]any{})}}
			handler := lockedResolutionHandler(t, []lock.Module{{Name: selected.Name, Version: selected.Version, Hash: tc.lockHash}})
			handler.vendorDir = t.TempDir()
			calls := 0
			handler.hub = &fakeHub{getManifest: func(_ context.Context, org, name, version string) (*ModuleManifest, error) {
				calls++
				if tc.hubDigest == "" {
					return nil, ErrModuleNotFound
				}
				return &ModuleManifest{Org: org, Name: name, Version: version, Digest: tc.hubDigest}, nil
			}}
			ctx := regapi.WithRegistry(newTestContext(), resolutionSnapshotRegistry{resolution: resolution})
			resolved, err := handler.resolveModules(ctx,
				[]DependencyDefinition{{Component: selected.Name, Version: "*"}},
				map[string]string{selected.Name: selected.Version}, resolution, state)
			if tc.wantError != "" {
				require.Error(t, err)
				require.True(t, strings.Contains(err.Error(), tc.wantError), err.Error())
				return
			}
			require.NoError(t, err)
			require.Len(t, resolved, 1)
			require.Equal(t, tc.wantDigest, resolved[0].Digest)
			require.Equal(t, tc.wantHub, calls > 0)
		})
	}
}
