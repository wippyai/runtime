// SPDX-License-Identifier: MPL-2.0
package topology

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/relay"
	topapi "github.com/wippyai/runtime/api/topology"
	sysrelay "github.com/wippyai/runtime/system/relay"
)

// startFixtureMonitorNetwork supplies live remote fixture targets to tests of
// local topology boundaries. It uses production admission/acknowledgments and
// native codec; it does not stand in for socket/host or missing-target tests.
// The original receiver retains captures and synchronous boundary callbacks.
func startFixtureMonitorNetwork(t *testing.T, local *Topology) {
	t.Helper()
	original := local.router
	var peers sync.Map
	local.router = monitorContextSender(func(ctx context.Context, p *relay.Package) error {
		if p.Target.Node == "" || p.Target.Node == local.localNodeID {
			return original.Send(p)
		}
		request := monitorCodecCopy(t, p, local.localNodeID)
		control, err := decodeRemoteMonitor(request, p.Target.Node)
		if err != nil || control == nil {
			relay.ReleasePackage(request)
			return original.Send(p)
		}
		defer relay.ReleasePackage(request)
		candidate := NewTopology(nodeExitBoundaryRouter(func(p *relay.Package) error { relay.ReleasePackage(p); return nil }), p.Target.Node)
		loaded, _ := peers.LoadOrStore(p.Target.Node, candidate)
		peer := loaded.(*Topology)
		if control.kind == topapi.MonitorRequest {
			err := peer.Register(control.target)
			if !errors.Is(err, topapi.ErrPIDAlreadyRegistered) {
				require.NoError(t, err)
			}
		}
		_, reply, err := peer.PrepareRemoteMonitorReply(request, 32)
		require.NoError(t, err)
		defer relay.ReleasePackage(reply)
		if err := original.Send(p); err != nil {
			return err
		}
		decoded := monitorCodecCopy(t, reply, control.target.Node)
		if err := local.monitorEndpoint.SendContext(ctx, decoded); err != nil {
			relay.ReleasePackage(decoded)
			t.Errorf("fixture reply: %v", err)
		}
		return nil
	})
	stop, err := local.StartRemoteMonitoring(context.Background(), sysrelay.NewNode(local.localNodeID), MonitorConfig{MaxPending: 32, RequestTimeout: time.Second})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, stop()) })
}
