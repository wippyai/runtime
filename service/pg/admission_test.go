// SPDX-License-Identifier: MPL-2.0

package pg

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/pid"
	pgapi "github.com/wippyai/runtime/api/service/pg"
	"github.com/wippyai/runtime/cluster/internode"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestDepartedPeerRemainsQuietBestEffort(t *testing.T) {
	svc, router, _ := newTestService()
	svc.ctxHolder.Store(&serviceCtx{ctx: t.Context()})
	router.sendErr = fmt.Errorf("admission: %w", internode.ErrNodeNotManaged)
	core, logs := observer.New(zap.DebugLevel)
	svc.logger = zap.New(core)
	svc.state.remote["gone"] = &remoteNode{nodeID: "gone", groups: make(map[string][]pid.PID)}
	member := mkNodePID("gone", "process", "1")
	changes := map[string][]pid.PID{"workers": {member}}
	for range 20 {
		svc.sendDiscover("gone")
		svc.sendSync("gone")
		svc.broadcastJoin(changes)
		svc.broadcastLeave(changes)
		require.Zero(t, svc.sendToMembers(pid.PID{}, "request", nil, []pid.PID{member}))
		for _, topic := range []string{pgapi.TopicJoin, pgapi.TopicLeave} {
			svc.retryQueue.attemptRetry(&retryEntry{targetNode: "gone", topic: topic,
				groups: []string{"workers"}, pids: []pid.PID{member}})
		}
	}
	require.Empty(t, svc.retryQueue.entries)
	require.Zero(t, logs.Len(), "departures must not cause a per-message log flood")
	require.True(t, svc.cbManager.GetCircuitBreaker("gone").Allow())
	router.sendErr = nil
	require.Equal(t, 1, svc.sendToMembers(pid.PID{}, "request", nil, []pid.PID{member}))
}
