// SPDX-License-Identifier: MPL-2.0

package crdt

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

// TestShardHash_ConcurrentDelete pins the Bug 13 fix: ShardHash must not
// panic with a nil-pointer dereference when entries are deleted while it
// is computing the hash. The previous implementation released the shard
// lock between key-snapshot and per-key lookup, so a delete in that
// window left sh.entries[key] == nil and panicked on e.Node access.
//
// Run with -race to catch any reintroduced lock-window splits.
func TestShardHash_ConcurrentDelete(t *testing.T) {
	st := NewState("node-a")

	// Seed with a substantial set so each shard has multiple entries —
	// the race window is per-key inside one shard, so a populated shard
	// is necessary to actually hit it.
	const seedKeys = 1000
	for i := 0; i < seedKeys; i++ {
		st.Overwrite(fmt.Sprintf("k-%05d", i), []byte("v"), int64(i))
	}

	// Spin a writer that churns: overwrite + delete in tight loop.
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		i := seedKeys
		for {
			select {
			case <-stop:
				return
			default:
			}
			key := fmt.Sprintf("k-%05d", i%seedKeys)
			st.Overwrite(key, []byte("v"), int64(i))
			if i%2 == 0 {
				st.Unregister(key, int64(i))
			}
			i++
		}
	}()

	// Hammer ShardHash across all shards from multiple goroutines for
	// 200ms. With the bug, this almost always panics within a few
	// milliseconds; with the fix, it completes cleanly.
	const readers = 8
	wg.Add(readers)
	deadline := time.Now().Add(200 * time.Millisecond)
	for r := 0; r < readers; r++ {
		go func() {
			defer wg.Done()
			for time.Now().Before(deadline) {
				for s := 0; s < ShardCount; s++ {
					_ = st.ShardHash(s)
				}
			}
		}()
	}

	// Wait for readers, then stop the writer.
	wgReaders := make(chan struct{})
	go func() { time.Sleep(220 * time.Millisecond); close(wgReaders) }()
	<-wgReaders
	close(stop)
	wg.Wait()
}
