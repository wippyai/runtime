package topology

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/runtime"
)

func TestShardIndex(t *testing.T) {
	t.Run("deterministic", func(t *testing.T) {
		key := "{host|process1}"
		idx1 := shardIndex(key)
		idx2 := shardIndex(key)
		assert.Equal(t, idx1, idx2)
	})

	t.Run("bounded", func(t *testing.T) {
		for i := 0; i < 1000; i++ {
			key := fmt.Sprintf("{host|process%d}", i)
			idx := shardIndex(key)
			assert.Less(t, idx, uint32(numShards))
		}
	})

	t.Run("distribution", func(t *testing.T) {
		counts := make([]int, numShards)
		for i := 0; i < 10000; i++ {
			key := fmt.Sprintf("{host|p%d}", i)
			idx := shardIndex(key)
			counts[idx]++
		}
		// Check that all shards get some entries
		for i, c := range counts {
			assert.Greater(t, c, 0, "shard %d has no entries", i)
		}
	})
}

func findSameShardPIDs() (pid.PID, pid.PID) {
	// Find two PIDs that hash to the same shard
	for i := 0; i < 10000; i++ {
		p1 := pid.PID{Host: "host", UniqID: fmt.Sprintf("a%d", i)}
		p1.Precomputed()
		for j := i + 1; j < 10000; j++ {
			p2 := pid.PID{Host: "host", UniqID: fmt.Sprintf("b%d", j)}
			p2.Precomputed()
			if shardIndex(p1.String()) == shardIndex(p2.String()) {
				return p1, p2
			}
		}
	}
	panic("could not find same-shard PIDs")
}

func findDifferentShardPIDs() (pid.PID, pid.PID) {
	// Find two PIDs that hash to different shards
	p1 := pid.PID{Host: "host", UniqID: "x0"}
	p1.Precomputed()
	for i := 1; i < 10000; i++ {
		p2 := pid.PID{Host: "host", UniqID: fmt.Sprintf("y%d", i)}
		p2.Precomputed()
		if shardIndex(p1.String()) != shardIndex(p2.String()) {
			return p1, p2
		}
	}
	panic("could not find different-shard PIDs")
}

func TestSharding_SameShard(t *testing.T) {
	t.Run("monitor same shard", func(t *testing.T) {
		upstream := newMockUpstream()
		topo := NewTopology(upstream, "local")
		p1, p2 := findSameShardPIDs()

		require.NoError(t, topo.Register(p1))
		require.NoError(t, topo.Register(p2))

		assert.Equal(t, shardIndex(p1.String()), shardIndex(p2.String()))

		err := topo.Monitor(p1, p2)
		require.NoError(t, err)

		// Verify relationship by checking notification on Complete
		topo.Complete(p2, &runtime.Result{})
		assert.Len(t, upstream.getSends(p1), 1, "p1 should receive notification when p2 completes")
	})

	t.Run("link same shard", func(t *testing.T) {
		topo := NewTopology(&discardReceiver{}, "local")
		p1, p2 := findSameShardPIDs()

		require.NoError(t, topo.Register(p1))
		require.NoError(t, topo.Register(p2))

		err := topo.Link(p1, p2)
		require.NoError(t, err)

		links1 := topo.GetLinks(p1)
		links2 := topo.GetLinks(p2)

		assert.Len(t, links1, 1)
		assert.Len(t, links2, 1)
	})
}

func TestSharding_DifferentShards(t *testing.T) {
	t.Run("monitor different shards", func(t *testing.T) {
		upstream := newMockUpstream()
		topo := NewTopology(upstream, "local")
		p1, p2 := findDifferentShardPIDs()

		require.NoError(t, topo.Register(p1))
		require.NoError(t, topo.Register(p2))

		assert.NotEqual(t, shardIndex(p1.String()), shardIndex(p2.String()))

		err := topo.Monitor(p1, p2)
		require.NoError(t, err)

		// Verify relationship by checking notification on Complete
		topo.Complete(p2, &runtime.Result{})
		assert.Len(t, upstream.getSends(p1), 1, "p1 should receive notification when p2 completes")
	})

	t.Run("link different shards", func(t *testing.T) {
		topo := NewTopology(&discardReceiver{}, "local")
		p1, p2 := findDifferentShardPIDs()

		require.NoError(t, topo.Register(p1))
		require.NoError(t, topo.Register(p2))

		err := topo.Link(p1, p2)
		require.NoError(t, err)

		links1 := topo.GetLinks(p1)
		links2 := topo.GetLinks(p2)

		assert.Len(t, links1, 1)
		assert.Len(t, links2, 1)
	})
}

func TestSharding_LockOrdering(t *testing.T) {
	t.Run("concurrent cross-shard links no deadlock", func(t *testing.T) {
		topo := NewTopology(&discardReceiver{}, "local")

		// Create many processes across different shards
		pids := make([]pid.PID, 100)
		for i := range pids {
			pids[i] = pid.PID{Host: "host", UniqID: fmt.Sprintf("p%d", i)}
			pids[i].Precomputed()
			require.NoError(t, topo.Register(pids[i]))
		}

		// Concurrently link in both directions
		var wg sync.WaitGroup
		for i := 0; i < 50; i++ {
			wg.Add(2)
			p1, p2 := pids[i], pids[99-i]

			go func() {
				defer wg.Done()
				_ = topo.Link(p1, p2)
			}()

			go func() {
				defer wg.Done()
				_ = topo.Link(p2, p1)
			}()
		}

		done := make(chan struct{})
		go func() {
			wg.Wait()
			close(done)
		}()

		select {
		case <-done:
			// Success - no deadlock
		case <-time.After(5 * time.Second):
			t.Fatal("deadlock detected in concurrent cross-shard links")
		}
	})

	t.Run("concurrent cross-shard monitors no deadlock", func(t *testing.T) {
		topo := NewTopology(&discardReceiver{}, "local")

		pids := make([]pid.PID, 100)
		for i := range pids {
			pids[i] = pid.PID{Host: "host", UniqID: fmt.Sprintf("m%d", i)}
			pids[i].Precomputed()
			require.NoError(t, topo.Register(pids[i]))
		}

		var wg sync.WaitGroup
		for i := 0; i < 50; i++ {
			wg.Add(2)
			p1, p2 := pids[i], pids[99-i]

			go func() {
				defer wg.Done()
				_ = topo.Monitor(p1, p2)
			}()

			go func() {
				defer wg.Done()
				_ = topo.Monitor(p2, p1)
			}()
		}

		done := make(chan struct{})
		go func() {
			wg.Wait()
			close(done)
		}()

		select {
		case <-done:
			// Success
		case <-time.After(5 * time.Second):
			t.Fatal("deadlock detected in concurrent cross-shard monitors")
		}
	})
}

func TestSharding_Complete(t *testing.T) {
	t.Run("complete cleans up cross-shard references", func(t *testing.T) {
		upstream := newMockUpstream()
		topo := NewTopology(upstream, "local")
		p1, p2 := findDifferentShardPIDs()

		require.NoError(t, topo.Register(p1))
		require.NoError(t, topo.Register(p2))

		require.NoError(t, topo.Monitor(p1, p2))
		require.NoError(t, topo.Link(p1, p2))

		// Verify links exist
		assert.Len(t, topo.GetLinks(p1), 1)
		assert.Len(t, topo.GetLinks(p2), 1)

		// Complete p2
		topo.Complete(p2, &runtime.Result{})

		// Verify p1 received notification
		assert.Len(t, upstream.getSends(p1), 1, "p1 should receive notification")

		// Verify cleanup - links should be removed
		assert.Len(t, topo.GetLinks(p1), 0, "p1 links should be cleaned up")
	})

	t.Run("concurrent complete no race", func(t *testing.T) {
		topo := NewTopology(&discardReceiver{}, "local")

		pids := make([]pid.PID, 100)
		for i := range pids {
			pids[i] = pid.PID{Host: "host", UniqID: fmt.Sprintf("c%d", i)}
			pids[i].Precomputed()
			require.NoError(t, topo.Register(pids[i]))
		}

		// Create some links
		for i := 0; i < 50; i++ {
			_ = topo.Link(pids[i], pids[99-i])
		}

		// Complete all concurrently
		var wg sync.WaitGroup
		for i := range pids {
			wg.Add(1)
			p := pids[i]
			go func() {
				defer wg.Done()
				topo.Complete(p, &runtime.Result{})
			}()
		}

		done := make(chan struct{})
		go func() {
			wg.Wait()
			close(done)
		}()

		select {
		case <-done:
			// Success
		case <-time.After(5 * time.Second):
			t.Fatal("deadlock or hang in concurrent complete")
		}
	})
}

func TestSharding_StatePooling(t *testing.T) {
	t.Run("state is recycled", func(t *testing.T) {
		topo := NewTopology(&discardReceiver{}, "local")

		// Register and complete many processes
		for i := 0; i < 1000; i++ {
			p := pid.PID{Host: "host", UniqID: fmt.Sprintf("r%d", i)}
			p.Precomputed()
			require.NoError(t, topo.Register(p))
			topo.Complete(p, &runtime.Result{})
		}

		// Should not panic or have issues
	})

	t.Run("large maps are discarded", func(t *testing.T) {
		topo := NewTopology(&discardReceiver{}, "local")

		main := pid.PID{Host: "host", UniqID: "main"}
		main.Precomputed()
		require.NoError(t, topo.Register(main))

		// Create many watchers to grow the map
		watchers := make([]pid.PID, 50)
		for i := range watchers {
			watchers[i] = pid.PID{Host: "host", UniqID: fmt.Sprintf("w%d", i)}
			watchers[i].Precomputed()
			require.NoError(t, topo.Register(watchers[i]))
			require.NoError(t, topo.Monitor(watchers[i], main))
		}

		// Complete main - large maps should be discarded
		topo.Complete(main, &runtime.Result{})

		// Register a new process - should work fine
		p := pid.PID{Host: "host", UniqID: "new"}
		p.Precomputed()
		require.NoError(t, topo.Register(p))
	})
}
