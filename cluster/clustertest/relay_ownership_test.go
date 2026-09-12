// SPDX-License-Identifier: MPL-2.0
package clustertest

import (
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/relay"
	"testing"
)

func TestRelayRouterRefusalPreservesCallerOwnership(t *testing.T) {
	for _, mode := range []string{"partition", "missing-node", "missing-host"} {
		t.Run(mode, func(t *testing.T) {
			router := newRelayRouter()
			if mode == "partition" {
				router.mesh = newMesh()
				router.mesh.setDown("destination", true)
			}
			if mode == "missing-host" {
				router.register("destination", "other", nil)
			}
			pkg := relay.NewServicePackage("source", "sender", "destination", "missing", "request")
			defer relay.ReleasePackage(pkg)
			before := pkg.Source
			require.Error(t, router.Send(pkg))
			require.Equal(t, before, pkg.Source, "refusal cannot reset/release caller-owned package")
			require.Len(t, pkg.Messages, 1)
			require.Equal(t, "request", pkg.Messages[0].Topic)
		})
	}
}
