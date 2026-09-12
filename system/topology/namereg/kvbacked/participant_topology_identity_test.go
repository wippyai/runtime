// SPDX-License-Identifier: MPL-2.0
package kvbacked

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	topologyapi "github.com/wippyai/runtime/api/topology"
	systopology "github.com/wippyai/runtime/system/topology"
)

type monitorDiscardRouter struct{}

func (monitorDiscardRouter) Send(pkg *relay.Package) error { relay.ReleasePackage(pkg); return nil }

func TestParticipantOwnsTopologyMonitorIdentity(t *testing.T) {
	for _, duplicate := range []bool{false, true} {
		t.Run(map[bool]string{false: "owned", true: "existing-owner"}[duplicate], func(t *testing.T) {
			_, engine := newParticipantTestInventory(t, 4)
			service := NewService(engine, "member", nil, nil)
			service.ConfigureStrong(StrongDeps{Incarnation: "one"})
			require.NoError(t, service.ConfigureParticipation(4))
			topo := systopology.NewTopology(monitorDiscardRouter{}, "member")
			service.SetTopology(topo)
			// This fixture checks topology registration ownership. Remote
			// admission is covered by the native multi-process actor proof.
			require.NoError(t, topo.Register(service.self))
			topo.Remove(service.self)
			if duplicate {
				require.NoError(t, topo.Register(service.self))
			}
			config := ParticipantEndpointConfig{MaxEntries: 8, MaxValueBytes: 8192, MaxWireBytes: 16384, MaxConcurrentRequests: 2, RequestTimeout: time.Second, RefreshInterval: time.Hour}
			mesh := &participantTestMesh{nodes: make(map[pid.NodeID]relay.ContextSender)}
			endpoint, err := NewParticipantEndpoint(context.Background(), service, mesh, func(context.Context) (pid.NodeID, error) { return "member", nil }, config)
			require.NoError(t, err)
			mesh.nodes["member"] = endpoint
			t.Cleanup(func() { require.NoError(t, endpoint.Stop(context.Background())); topo.Remove(service.self) })
			_, bindErr := endpoint.BindLocal(service.self)
			require.ErrorIs(t, bindErr, relay.ErrBindingRetired)
			var bound relay.ContextSender
			err = endpoint.Start(context.Background())
			if duplicate {
				require.ErrorIs(t, err, topologyapi.ErrPIDAlreadyRegistered)
			} else {
				require.NoError(t, err)
				bound, bindErr = endpoint.BindLocal(service.self)
				require.NoError(t, bindErr)
				_, wrongErr := endpoint.BindLocal(pid.PID{Node: "other", Host: RegistryHostID})
				require.ErrorIs(t, wrongErr, relay.ErrBindingTarget)
				event := relay.NewPackage(pid.PID{Node: "remote"}, service.self, topologyapi.TopicEvents)
				require.NoError(t, bound.SendContext(context.Background(), event))
				require.ErrorIs(t, topo.Register(service.self), topologyapi.ErrPIDAlreadyRegistered, "registry caller identity was not installed")
			}
			require.NoError(t, endpoint.Stop(context.Background()))
			if bound != nil {
				pkg := relay.NewPackage(pid.PID{}, service.self, topologyapi.TopicEvents)
				require.ErrorIs(t, bound.SendContext(context.Background(), pkg), relay.ErrBindingRetired)
				require.Len(t, pkg.Messages, 1, "stopped binding retains caller ownership")
				relay.ReleasePackage(pkg)
			}
			err = topo.Register(service.self)
			if duplicate {
				require.ErrorIs(t, err, topologyapi.ErrPIDAlreadyRegistered, "failed startup removed someone else's identity")
			} else {
				require.NoError(t, err, "shutdown retained monitor identity")
				topo.Remove(service.self)
			}
		})
	}
}
