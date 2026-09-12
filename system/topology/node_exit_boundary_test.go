// SPDX-License-Identifier: MPL-2.0

package topology

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	topapi "github.com/wippyai/runtime/api/topology"
)

type nodeExitBoundaryRouter func(*relay.Package) error

func (r nodeExitBoundaryRouter) Send(pkg *relay.Package) error { return r(pkg) }

func TestNodeExitNotificationCannotEraseLaterRegistration(t *testing.T) {
	old := pid.PID{Node: "remote", Host: "app", UniqID: "old"}
	fresh := pid.PID{Node: "remote", Host: "app", UniqID: "fresh"}
	observer := pid.PID{Node: "local", Host: "app", UniqID: "observer"}
	var topo *Topology
	called := false
	topo = NewTopology(nodeExitBoundaryRouter(func(pkg *relay.Package) error {
		defer relay.ReleasePackage(pkg)
		isExit := false
		for _, msg := range pkg.Messages {
			for _, pl := range msg.Payloads {
				if event, ok := pl.Data().(*topapi.ExitEvent); ok && event.Kind == topapi.LinkDown {
					isExit = true
				}
			}
		}
		if called || !isExit {
			return nil
		}
		called = true
		// A consumer reacts synchronously to LinkDown. No sleeps or scheduler
		// assumptions: this is the exact notification-to-cleanup boundary.
		require.NoError(t, topo.Register(fresh))
		require.NoError(t, topo.Monitor(observer, fresh))
		return nil
	}), "local")
	startFixtureMonitorNetwork(t, topo)
	for _, p := range []pid.PID{old, observer} {
		require.NoError(t, topo.Register(p))
	}
	require.NoError(t, topo.Monitor(observer, old))
	topo.HandleNodeExit("remote", nil)
	require.True(t, called)
	key := fresh.String()
	sh := topo.getShard(key)
	sh.mu.RLock()
	_, exists := sh.processes[key]
	sh.mu.RUnlock()
	require.True(t, exists)
	index, indexed := topo.nodeIndex.Load("remote")
	require.True(t, indexed, "node exit must not drop the newer process's index")
	nk := index.(*nodeKeys)
	nk.mu.Lock()
	keys := append([]string(nil), nk.keys...)
	nk.mu.Unlock()
	require.Contains(t, keys, key)
	observerKey := observer.String()
	sh = topo.getShard(observerKey)
	sh.mu.RLock()
	_, watching := sh.processes[observerKey].watching[key]
	sh.mu.RUnlock()
	require.True(t, watching, "a monitor installed after notification must survive")
}

func TestNodeIndexLastRemovalRacesRegistration(t *testing.T) {
	topo := NewTopology(nodeExitBoundaryRouter(func(pkg *relay.Package) error {
		relay.ReleasePackage(pkg)
		return nil
	}), "local")
	for i := range 1000 {
		old := pid.PID{Node: "local", Host: "app", UniqID: fmt.Sprintf("old-%d", i)}
		fresh := pid.PID{Node: "local", Host: "app", UniqID: fmt.Sprintf("new-%d", i)}
		require.NoError(t, topo.Register(old))
		start := make(chan struct{})
		registered := make(chan error, 1)
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			topo.Remove(old)
		}()
		go func() {
			defer wg.Done()
			<-start
			registered <- topo.Register(fresh)
		}()
		close(start)
		wg.Wait()
		require.NoError(t, <-registered)
		index, exists := topo.nodeIndex.Load("local")
		require.True(t, exists)
		nk := index.(*nodeKeys)
		nk.mu.Lock()
		keys := append([]string(nil), nk.keys...)
		nk.mu.Unlock()
		require.Equal(t, []string{fresh.String()}, keys)
		topo.Remove(fresh)
	}
}

func TestNodeExitConcurrentRegistrationAfterDetachSurvives(t *testing.T) {
	topo := NewTopology(nodeExitBoundaryRouter(func(pkg *relay.Package) error {
		relay.ReleasePackage(pkg)
		return nil
	}), "local")
	old := pid.PID{Node: "remote", Host: "app", UniqID: "old"}
	require.NoError(t, topo.Register(old))

	var fresh pid.PID
	for i := 0; ; i++ {
		candidate := pid.PID{Node: "remote", Host: "app", UniqID: fmt.Sprintf("fresh-%d", i)}
		if shardIndex(candidate.String()) == numShards-1 {
			fresh = candidate
			break
		}
	}

	// Hold the first shard so retirement is paused after its index detach and
	// allow a new registration to publish in a later shard.
	gate := &topo.shards[0]
	gate.mu.Lock()
	done := make(chan struct{})
	go func() {
		topo.HandleNodeExit("remote", nil)
		close(done)
	}()

	deadline := time.Now().Add(time.Second)
	for {
		if _, exists := topo.nodeIndex.Load("remote"); !exists {
			break
		}
		if time.Now().After(deadline) {
			gate.mu.Unlock()
			t.Fatal("node index was not detached")
		}
		time.Sleep(time.Millisecond)
	}
	require.NoError(t, topo.Register(fresh))
	gate.mu.Unlock()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("node exit did not finish")
	}

	sh := topo.getShard(fresh.String())
	sh.mu.RLock()
	_, exists := sh.processes[fresh.String()]
	sh.mu.RUnlock()
	require.True(t, exists, "registration after the retirement boundary must survive")
	index, indexed := topo.nodeIndex.Load("remote")
	require.True(t, indexed)
	nk := index.(*nodeKeys)
	nk.mu.Lock()
	keys := append([]string(nil), nk.keys...)
	nk.mu.Unlock()
	require.Equal(t, []string{fresh.String()}, keys)
}

// Isolate the unrelated-process scan. There are no notifications or failed-node
// entries, so this measures the per-shard sweep without routing overhead.
func BenchmarkNodeExitUnrelatedTopology(b *testing.B) {
	for _, count := range []int{0, 1000, 100000} {
		b.Run(fmt.Sprintf("processes=%d", count), func(b *testing.B) {
			topo := NewTopology(nodeExitBoundaryRouter(func(pkg *relay.Package) error {
				relay.ReleasePackage(pkg)
				return nil
			}), "local")
			for i := range count {
				p := pid.PID{Node: "local", Host: "app", UniqID: fmt.Sprint(i)}
				if err := topo.Register(p); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				topo.HandleNodeExit("absent", nil)
			}
		})
	}
}
