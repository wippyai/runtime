// SPDX-License-Identifier: MPL-2.0
package global

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/topology"
)

func TestGlobalReaperRequiresConfirmedOwnerExit(t *testing.T) {
	for _, mode := range []string{"exit", "linkdown", "other-process", "other-peer"} {
		t.Run(mode, func(t *testing.T) {
			fsm := NewFSM()
			svc := NewService(newDirectApplyRaft(fsm, true), fsm, &nopBus{}, nil, &nopRouter{}, &fakeMembership{local: "local", ids: []string{"local"}}, "local", noopLogger(), nil, nil, nil)
			owner := makePID("owner-node", "app", "owner")
			_, err := svc.Register(context.Background(), "protected", owner)
			require.NoError(t, err)
			svc.monitoredPIDs.Store(owner.String(), struct{}{})
			source, ingress, kind := owner, owner.Node, topology.Exit
			switch mode {
			case "linkdown":
				source, ingress, kind = topology.SystemPID, "", topology.LinkDown
			case "other-process":
				source = makePID(owner.Node, "app", "other")
			case "other-peer":
				ingress = "other-node"
			}
			pkg := relay.NewPackage(source, makePID("local", HostID, ""), topology.TopicEvents, payload.New(&topology.ExitEvent{From: owner, Kind: kind}))
			pkg.ReceivedFrom = ingress
			require.NoError(t, svc.Send(pkg))
			_, found := fsm.State().Lookup("protected")
			require.Equal(t, mode != "exit", found, "only an owner-matched process exit can reap names")
		})
	}
}
