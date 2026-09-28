// SPDX-License-Identifier: MPL-2.0

package global

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/pid"
	globalapi "github.com/wippyai/runtime/api/topology/namereg/global"
	systopology "github.com/wippyai/runtime/system/topology"
	"github.com/wippyai/runtime/system/topology/namereg/eventual"
)

func newIndependentBindings(t *testing.T, name string) (*systopology.PIDRegistry, *eventual.Service, pid.PID, pid.PID) {
	t.Helper()
	local := systopology.NewPIDRegistry()
	evt := eventual.NewService(eventual.Config{LocalNodeID: "node-1"})
	localPID := makePID("node-1", "process", "local")
	eventualPID := makePID("node-1", "process", "eventual")
	_, err := local.Register(name, localPID)
	require.NoError(t, err)
	_, err = evt.Register(name, eventualPID)
	require.NoError(t, err)
	return local, evt, localPID, eventualPID
}

func TestStrongPromotionPreservesIndependentBindings(t *testing.T) {
	svc := newJoinTestService(t)
	local, evt, localPID, eventualPID := newIndependentBindings(t, "shared")
	local.SetGlobalRegistry(svc)
	local.SetEventualRegistry(evt)
	owner := makePID("node-1", "process", "strong")
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	out, err := svc.RegisterScope(ctx, "shared", owner, globalapi.Strong)
	require.NoError(t, err)
	require.Equal(t, globalapi.RegisterStateActive, out.State)
	got, found := local.Lookup("shared")
	require.True(t, found)
	require.True(t, got.Equal(owner), "composed lookup must prefer the active global owner")
	got, found = local.LookupLocal("shared")
	require.True(t, found)
	require.True(t, got.Equal(localPID), "Strong promotion must preserve LOCAL ownership")
	result, err := evt.Lookup(ctx, "shared")
	require.NoError(t, err)
	require.True(t, result.Found)
	require.True(t, result.PID.Equal(eventualPID), "Strong promotion must preserve EVENTUAL ownership")
	removed, err := svc.UnregisterScope(ctx, "shared", globalapi.Strong)
	require.NoError(t, err)
	require.True(t, removed)
	got, found = local.Lookup("shared")
	require.True(t, found)
	require.True(t, got.Equal(eventualPID), "global removal reveals the independent EVENTUAL binding")
}

func TestJoinBarrierPreservesIndependentBindings(t *testing.T) {
	for _, pending := range []bool{false, true} {
		name := "active"
		if pending {
			name = "pending"
		}
		t.Run(name, func(t *testing.T) {
			svc := newJoinTestService(t)
			svc.fsm.SetOnPending(nil)
			owner := makePID("node-2", "process", "strong")
			if pending {
				openPending(t, svc.fsm, "shared", owner, "node-2", []pid.NodeID{"node-1", "node-2"}, 600)
			} else {
				seedActiveStrong(t, svc.fsm, "shared", owner, []pid.NodeID{"node-1"}, 600)
			}
			local, evt, localPID, eventualPID := newIndependentBindings(t, "shared")
			require.NoError(t, svc.runJoinBarrier(svc.nodeEpoch.Load()))
			require.NoError(t, svc.runJoinBarrier(svc.nodeEpoch.Load()))
			require.True(t, svc.NameReady())
			reserved, found := svc.IsStrongReserved("shared")
			require.True(t, found)
			require.True(t, reserved.Equal(owner))
			got, found := local.LookupLocal("shared")
			require.True(t, found)
			require.True(t, got.Equal(localPID))
			result, err := evt.Lookup(context.Background(), "shared")
			require.NoError(t, err)
			require.True(t, result.Found)
			require.True(t, result.PID.Equal(eventualPID))
		})
	}
}
