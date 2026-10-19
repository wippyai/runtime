// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/boot/deps/lock"
)

func TestResidentManifestResolutionEvidenceBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name                            string
		durable, owned, digest, offline bool
		wantHub, wantError              bool
	}{
		{name: "durable resident", durable: true, owned: true, digest: true},
		{name: "legacy selection", owned: true, digest: true, wantHub: true},
		{name: "no resident entries", durable: true, digest: true, wantHub: true},
		{name: "no artifact digest", durable: true, owned: true, wantHub: true},
		{name: "offline resident is not artifact evidence", durable: true, owned: true, digest: true, offline: true, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			selected := regapi.ResolvedModule{Name: "acme/resident", Version: "1.0.0", Source: moduleSourceHub}
			if tc.digest {
				selected.Digest = "sha256:" + lockedResolutionDigest
			}
			resolution := &regapi.DependencyResolution{
				Roots:   []regapi.DependencyRoot{{ID: "host.deps:resident", Component: selected.Name, Version: selected.Version}},
				Modules: []regapi.ResolvedModule{selected},
			}
			if tc.durable {
				resolution.Deployment = &regapi.Deployment{Root: selected.Name, Modules: []regapi.ResolvedModule{selected}}
			}
			resolution = resolution.Canonical()
			require.Equal(t, tc.digest, resolution.Valid(), "digest-free deployment records must not authorize resident reuse")
			state := regapi.State{{ID: regapi.NewID("acme.resident", "definition"), Kind: regapi.NamespaceDefinition,
				Data: payload.New(map[string]any{})}}
			if tc.owned {
				state[0].Registry.Owner = selected.Name
			}
			calls := 0
			handler := lockedResolutionHandler(t, []lock.Module{{Name: selected.Name, Version: selected.Version}})
			handler.vendorDir = t.TempDir()
			handler.hub = &fakeHub{getManifest: func(_ context.Context, org, name, version string) (*ModuleManifest, error) {
				calls++
				return &ModuleManifest{Org: org, Name: name, Version: version, Digest: selected.Digest}, nil
			}}
			ctx := regapi.WithRegistry(newTestContext(), resolutionSnapshotRegistry{resolution: resolution})
			if tc.offline {
				ctx = regapi.WithDependencyAccess(ctx, regapi.DependencyAccessVerifiedOffline)
			}
			resolved, err := handler.resolveModules(ctx,
				[]DependencyDefinition{{Component: selected.Name, Version: selected.Version}},
				map[string]string{selected.Name: selected.Version}, resolution, state)
			if tc.wantError {
				require.Error(t, err, "resident declarations cannot replace verified artifact bytes during offline restore")
				require.Empty(t, resolved)
			} else {
				require.NoError(t, err)
				require.Len(t, resolved, 1)
				require.Equal(t, selected.Digest, resolved[0].Digest)
			}
			if tc.wantHub {
				require.Equal(t, 1, calls)
			} else {
				require.Zero(t, calls)
			}
		})
	}
}
