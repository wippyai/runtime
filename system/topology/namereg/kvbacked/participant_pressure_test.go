// SPDX-License-Identifier: MPL-2.0

package kvbacked

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
)

type participantPressureMesh struct {
	participantTestMesh
	blocked   atomic.Bool
	delivered map[pid.NodeID]*atomic.Uint64 // populated before any endpoint starts
}

func (m *participantPressureMesh) SendContext(ctx context.Context, pkg *relay.Package) error {
	if m.blocked.Load() {
		return errors.New("pressure harness link unavailable")
	}
	counter := m.delivered[pkg.Target.Node]
	success := len(pkg.Messages) == 1 && pkg.Messages[0].Topic == participantSnapshotResponseTopic
	err := m.participantTestMesh.SendContext(ctx, pkg)
	if err == nil && success && counter != nil {
		counter.Add(1)
	}
	return err
}

// This is 100 live naming feeds, not 100 OS processes/native mesh nodes. It
// isolates bounded authority contention, coalescing and per-client recovery.
func TestParticipantHundredFeedsRecoverUnderBoundedAuthority(t *testing.T) {
	if testing.Short() {
		t.Skip("100-feed contention integration")
	}
	const clients = 100
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	_, engine := newParticipantTestInventory(t, clients+1)
	for i := 0; i < 1000; i++ {
		name := fmt.Sprintf("claim-%04d", i)
		owner := mkPID("authority", name)
		value, err := encode(activeValue{Name: name, PID: owner.String()})
		require.NoError(t, err)
		_, err = engine.Set(activeKey(name), value)
		require.NoError(t, err)
	}
	mesh := &participantPressureMesh{participantTestMesh: participantTestMesh{nodes: make(map[pid.NodeID]relay.ContextSender)}, delivered: make(map[pid.NodeID]*atomic.Uint64)}
	config := ParticipantEndpointConfig{MaxEntries: 1024, MaxValueBytes: 512 << 10, MaxWireBytes: 1 << 20, MaxConcurrentRequests: 8, MaxRedirects: 2, RequestTimeout: 2 * time.Second, RefreshInterval: time.Hour}
	resolve := func(context.Context) (pid.NodeID, error) { return "authority", nil }
	authority := NewService(engine, "authority", nil, nil)
	authority.ConfigureStrong(StrongDeps{Incarnation: "authority-one"})
	require.NoError(t, authority.ConfigureParticipation(clients+1))
	server, err := NewParticipantEndpoint(ctx, authority, mesh, resolve, config)
	require.NoError(t, err)
	endpoints := []*ParticipantEndpoint{server}
	ownEndpoint := func(endpoint *ParticipantEndpoint) {
		// Register after its engine cleanup so LIFO joins the endpoint first,
		// including an assertion failure during later fixture construction.
		t.Cleanup(func() {
			stop, done := context.WithTimeout(context.Background(), 10*time.Second)
			defer done()
			require.NoError(t, endpoint.Stop(stop))
		})
	}
	ownEndpoint(server)
	mesh.nodes["authority"] = server
	registries := make([]*Service, clients)
	ids := make([]string, clients)
	for i := range clients {
		id := fmt.Sprintf("client-%03d", i)
		ids[i] = id
		_, local := newParticipantTestInventory(t, clients+1)
		registry := NewService(local, id, nil, nil)
		registry.SetNonMember(func() bool { return true })
		registry.ConfigureStrong(StrongDeps{Incarnation: id + "-one", IsLeader: func() bool { return false }})
		require.NoError(t, registry.ConfigureParticipation(clients+1))
		endpoint, err := NewParticipantEndpoint(ctx, registry, mesh, resolve, config)
		require.NoError(t, err)
		endpoints = append(endpoints, endpoint)
		ownEndpoint(endpoint)
		registries[i] = registry
		mesh.nodes[id] = endpoint
		mesh.delivered[id] = &atomic.Uint64{}
	}
	require.NoError(t, server.Start(ctx))
	for _, endpoint := range endpoints[1:] {
		require.NoError(t, endpoint.Start(ctx))
	}
	warmStarted := time.Now()
	// One simultaneous burst, then retry only clients still needing a complete
	// successful refresh. Successful clients aren't needlessly rescheduled.
	recoverRound := func() {
		t.Helper()
		targets := make([]uint64, clients)
		for i, registry := range registries {
			targets[i] = mesh.delivered[ids[i]].Load() + 1
			for range 100 {
				registry.requestParticipantRefresh()
			}
		}
		require.Eventually(t, func() bool {
			complete := true
			for i, registry := range registries {
				if mesh.delivered[ids[i]].Load() < targets[i] || !registry.NameReady() {
					complete = false
					registry.requestParticipantRefresh()
				}
			}
			return complete
		}, 15*time.Second, 25*time.Millisecond)
	}
	recoverRound()
	runtime.GC()
	var warm, after runtime.MemStats
	runtime.ReadMemStats(&warm)
	warmGoroutines := runtime.NumGoroutine()
	warmTime := time.Since(warmStarted)
	mesh.blocked.Store(true)
	for _, registry := range registries {
		registry.requestParticipantRefresh()
	}
	require.Eventually(t, func() bool {
		for _, registry := range registries {
			if registry.NameReady() {
				return false
			}
		}
		return true
	}, 5*time.Second, 10*time.Millisecond)
	mesh.blocked.Store(false)
	recovered := time.Now()
	recoverRound()
	recoveryTime := time.Since(recovered)
	runtime.GC()
	runtime.ReadMemStats(&after)
	t.Logf("100 feeds/1000 claims: warm=%s recovery=%s heap=%d->%d goroutines=%d->%d", warmTime, recoveryTime, warm.HeapAlloc, after.HeapAlloc, warmGoroutines, runtime.NumGoroutine())
	members, _, err := authority.strong.participants.readSnapshot()
	require.NoError(t, err)
	require.Len(t, members, clients+1, "transport failure cannot retire participation")
	for _, registry := range registries {
		require.True(t, registry.NameReady())
	}
}
