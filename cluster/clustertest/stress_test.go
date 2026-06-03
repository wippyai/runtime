// SPDX-License-Identifier: MPL-2.0

package clustertest

import (
	"errors"
	"fmt"
	"strconv"
	"sync"
	"testing"
	"time"

	kvapi "github.com/wippyai/runtime/api/store/kv"
)

// TestStress_LinearizableCASCounter has many goroutines increment a shared key
// via CompareAndSwap from random nodes (forwarded to the leader). Raft
// linearizability means every successful CAS is exactly-once, so the final value
// must equal the total number of increments — no lost updates.
func TestStress_LinearizableCASCounter(t *testing.T) {
	if testing.Short() {
		t.Skip("real multi-node stress test")
	}
	c := NewCluster(t, 3)
	if _, err := c.Leader().KV.Set("counter", []byte("0")); err != nil {
		t.Fatalf("init: %v", err)
	}

	const workers, perWorker = 4, 15
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				node := c.Node((w + i) % len(c.Nodes())) // spread across nodes -> forwarding
				casIncrement(t, node, "counter")
			}
		}(w)
	}
	wg.Wait()

	want := workers * perWorker
	// Every node must converge to the exact count (no lost updates).
	deadline := time.Now().Add(10 * time.Second)
	for _, n := range c.Nodes() {
		for {
			e, err := n.KV.Get("counter")
			if err == nil {
				if v, _ := strconv.Atoi(string(e.Value)); v == want {
					break
				}
			}
			if time.Now().After(deadline) {
				e, _ := n.KV.Get("counter")
				t.Fatalf("node %s counter=%s, want %d (lost updates?)", n.ID, e.Value, want)
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
}

// casIncrement does a read-modify-write CAS loop until it commits one increment.
func casIncrement(t *testing.T, n *Node, key string) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		e, err := n.KV.Get(key)
		if err != nil {
			time.Sleep(5 * time.Millisecond)
			continue
		}
		v, _ := strconv.Atoi(string(e.Value))
		_, ok, err := n.KV.CompareAndSwap(key, e.Version, []byte(strconv.Itoa(v+1)))
		if err != nil {
			if errors.Is(err, kvapi.ErrKeyNotFound) {
				time.Sleep(5 * time.Millisecond)
			}
			continue
		}
		if ok {
			return
		}
		// version raced; retry
	}
	t.Errorf("casIncrement on %s timed out", n.ID)
}

// TestStress_ConcurrentDistinctKeys writes many distinct keys concurrently from
// all nodes and verifies every key is present and replicated everywhere.
func TestStress_ConcurrentDistinctKeys(t *testing.T) {
	if testing.Short() {
		t.Skip("real multi-node stress test")
	}
	c := NewCluster(t, 3)
	const workers, perWorker = 4, 20
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				key := fmt.Sprintf("k-%d-%d", w, i)
				node := c.Node((w + i) % len(c.Nodes()))
				if _, err := node.KV.Set(key, []byte("v")); err != nil {
					t.Errorf("set %s: %v", key, err)
					return
				}
			}
		}(w)
	}
	wg.Wait()

	deadline := time.Now().Add(15 * time.Second)
	for _, n := range c.Nodes() {
		for w := 0; w < workers; w++ {
			for i := 0; i < perWorker; i++ {
				key := fmt.Sprintf("k-%d-%d", w, i)
				for {
					if _, err := n.KV.Get(key); err == nil {
						break
					}
					if time.Now().After(deadline) {
						t.Fatalf("node %s missing %s", n.ID, key)
					}
					time.Sleep(10 * time.Millisecond)
				}
			}
		}
	}
}
