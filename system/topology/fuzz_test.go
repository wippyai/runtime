// SPDX-License-Identifier: MPL-2.0

package topology

import (
	"sync"
	"testing"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/runtime"
)

func FuzzTopology_ConcurrentOperations(f *testing.F) {
	// Seed corpus
	f.Add(uint8(0), uint8(1), uint8(2))
	f.Add(uint8(5), uint8(10), uint8(15))
	f.Add(uint8(255), uint8(128), uint8(64))

	f.Fuzz(func(_ *testing.T, op1, op2, op3 uint8) {
		upstream := newMockUpstream()
		topo := NewTopology(upstream, "local")

		// Create PIDs based on input
		pid1 := pid.PID{Host: "host", UniqID: string(rune('A' + (op1 % 26)))}
		pid1 = pid1.Precomputed()
		pid2 := pid.PID{Host: "host", UniqID: string(rune('a' + (op2 % 26)))}
		pid2 = pid2.Precomputed()

		// Register both
		_ = topo.Register(pid1)
		_ = topo.Register(pid2)

		var wg sync.WaitGroup

		// Concurrent operations based on input
		operations := []func(){
			func() { _ = topo.Monitor(pid1, pid2) },
			func() { _ = topo.Demonitor(pid1, pid2) },
			func() { _ = topo.Link(pid1, pid2) },
			func() { _ = topo.Unlink(pid1, pid2) },
			func() { _ = topo.GetLinks(pid1) },
			func() { topo.Complete(pid2, &runtime.Result{}) },
			func() { topo.Remove(pid1) },
			func() { _ = topo.Register(pid1) },
		}

		// Run 3 operations concurrently
		selected := []int{int(op1 % 8), int(op2 % 8), int(op3 % 8)}
		for _, idx := range selected {
			wg.Add(1)
			op := operations[idx]
			go func() {
				defer wg.Done()
				op()
			}()
		}
		wg.Wait()
	})
}

func FuzzTopology_MonitorLinkChurn(f *testing.F) {
	f.Add(uint16(100), uint8(5))
	f.Add(uint16(50), uint8(10))

	f.Fuzz(func(_ *testing.T, iterations uint16, processCount uint8) {
		if iterations > 1000 || processCount < 2 || processCount > 50 {
			return
		}

		upstream := newMockUpstream()
		topo := NewTopology(upstream, "local")

		// Create and register processes
		pids := make([]pid.PID, processCount)
		for i := range pids {
			pids[i] = pid.PID{Host: "host", UniqID: string([]byte{byte(i)})}
			pids[i] = pids[i].Precomputed()
			_ = topo.Register(pids[i])
		}

		var wg sync.WaitGroup

		// Concurrent monitor/demonitor/link/unlink cycles
		for i := uint16(0); i < iterations; i++ {
			from := pids[int(i)%len(pids)]
			to := pids[(int(i)+1)%len(pids)]

			wg.Add(4)
			go func() {
				defer wg.Done()
				_ = topo.Monitor(from, to)
			}()
			go func() {
				defer wg.Done()
				_ = topo.Demonitor(from, to)
			}()
			go func() {
				defer wg.Done()
				_ = topo.Link(from, to)
			}()
			go func() {
				defer wg.Done()
				_ = topo.Unlink(from, to)
			}()
		}
		wg.Wait()

		// Verify consistency - no deadlocks, no panics
		for _, p := range pids {
			_ = topo.GetLinks(p)
		}
	})
}

func FuzzTopology_CompleteUnderLoad(f *testing.F) {
	f.Add(uint8(10), uint8(3))
	f.Add(uint8(20), uint8(5))

	f.Fuzz(func(_ *testing.T, watcherCount, targetCount uint8) {
		if watcherCount < 1 || watcherCount > 50 || targetCount < 1 || targetCount > 20 {
			return
		}

		upstream := newMockUpstream()
		topo := NewTopology(upstream, "local")

		// Create watchers and targets
		watchers := make([]pid.PID, watcherCount)
		targets := make([]pid.PID, targetCount)

		for i := range watchers {
			watchers[i] = pid.PID{Host: "watcher", UniqID: string([]byte{byte(i)})}
			watchers[i] = watchers[i].Precomputed()
			_ = topo.Register(watchers[i])
		}

		for i := range targets {
			targets[i] = pid.PID{Host: "target", UniqID: string([]byte{byte(i)})}
			targets[i] = targets[i].Precomputed()
			_ = topo.Register(targets[i])
		}

		// Set up monitoring
		for _, w := range watchers {
			for _, t := range targets {
				_ = topo.Monitor(w, t)
			}
		}

		// Complete all targets concurrently
		var wg sync.WaitGroup
		for _, t := range targets {
			wg.Add(1)
			target := t
			go func() {
				defer wg.Done()
				topo.Complete(target, &runtime.Result{Value: payload.New("done")})
			}()
		}
		wg.Wait()
	})
}

func FuzzPIDRegistry_ConcurrentLookup(f *testing.F) {
	f.Add(uint8(10))
	f.Add(uint8(50))

	f.Fuzz(func(_ *testing.T, count uint8) {
		if count < 1 || count > 100 {
			return
		}

		reg := NewPIDRegistry()

		// Pre-register some entries
		for i := uint8(0); i < count; i++ {
			p := pid.PID{Host: "host", UniqID: string([]byte{i})}
			name := string([]byte{i})
			_, _ = reg.Register(name, p)
		}

		// Concurrent lookups
		var wg sync.WaitGroup
		for i := uint8(0); i < count; i++ {
			wg.Add(1)
			name := string([]byte{i})
			go func() {
				defer wg.Done()
				_, _ = reg.Lookup(name)
			}()
		}
		wg.Wait()
	})
}
