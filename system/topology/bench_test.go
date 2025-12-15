package topology

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"testing"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	runtimeapi "github.com/wippyai/runtime/api/runtime"
)

type discardReceiver struct{}

func (d *discardReceiver) Send(*relay.Package) error { return nil }

func BenchmarkRegister(b *testing.B) {
	topo := NewTopology(&discardReceiver{}, "local")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p := pid.PID{Host: "host", UniqID: fmt.Sprintf("%d", i)}.Precomputed()
		_ = topo.Register(p)
	}
}

func BenchmarkRegisterPrecomputed(b *testing.B) {
	topo := NewTopology(&discardReceiver{}, "local")
	pids := make([]pid.PID, b.N)
	for i := range pids {
		pids[i] = pid.PID{Host: "host", UniqID: fmt.Sprintf("%d", i)}.Precomputed()
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = topo.Register(pids[i])
	}
}

func BenchmarkRemove(b *testing.B) {
	topo := NewTopology(&discardReceiver{}, "local")
	pids := make([]pid.PID, b.N)
	for i := range pids {
		pids[i] = pid.PID{Host: "host", UniqID: fmt.Sprintf("%d", i)}.Precomputed()
		_ = topo.Register(pids[i])
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		topo.Remove(pids[i])
	}
}

func BenchmarkRegisterAndRemove(b *testing.B) {
	topo := NewTopology(&discardReceiver{}, "local")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pid := pid.PID{Host: "host", UniqID: fmt.Sprintf("%d", i)}.Precomputed()
		_ = topo.Register(pid)
		topo.Remove(pid)
	}
}

func BenchmarkRegisterAndComplete(b *testing.B) {
	topo := NewTopology(&discardReceiver{}, "local")
	result := &runtimeapi.Result{}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pid := pid.PID{Host: "host", UniqID: fmt.Sprintf("%d", i)}.Precomputed()
		_ = topo.Register(pid)
		topo.Complete(pid, result)
	}
}

func BenchmarkRegisterRemove100k(b *testing.B) {
	if testing.Short() {
		b.Skip("skipping stress benchmark in short mode")
	}
	const numProcesses = 100000

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		topo := NewTopology(&discardReceiver{}, "local")
		pids := make([]pid.PID, numProcesses)

		// Register all
		for i := 0; i < numProcesses; i++ {
			pids[i] = pid.PID{Host: "host", UniqID: fmt.Sprintf("%d", i)}.Precomputed()
			_ = topo.Register(pids[i])
		}

		// Remove all
		for i := 0; i < numProcesses; i++ {
			topo.Remove(pids[i])
		}
	}
}

func BenchmarkNotify(b *testing.B) {
	topo := NewTopology(&discardReceiver{}, "local")

	monitored := pid.PID{Host: "host", UniqID: "monitored"}.Precomputed()
	_ = topo.Register(monitored)

	watchers := make([]pid.PID, 10)
	for i := range watchers {
		watchers[i] = pid.PID{Host: "host", UniqID: fmt.Sprintf("watcher-%d", i)}.Precomputed()
		_ = topo.Register(watchers[i])
		_ = topo.Monitor(watchers[i], monitored)
	}

	result := &runtimeapi.Result{Value: payload.New("test")}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		topo.Complete(monitored, result)
	}
}

func BenchmarkNotifyWithLinks(b *testing.B) {
	topo := NewTopology(&discardReceiver{}, "local")

	main := pid.PID{Host: "host", UniqID: "main"}.Precomputed()
	_ = topo.Register(main)

	linked := make([]pid.PID, 10)
	for i := range linked {
		linked[i] = pid.PID{Host: "host", UniqID: fmt.Sprintf("linked-%d", i)}.Precomputed()
		_ = topo.Register(linked[i])
		_ = topo.Link(main, linked[i])
	}

	result := &runtimeapi.Result{Value: payload.New("test"), Error: fmt.Errorf("crash")}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		topo.Complete(main, result)
	}
}

func BenchmarkLink(b *testing.B) {
	topo := NewTopology(&discardReceiver{}, "local")

	pids := make([]pid.PID, b.N+1)
	for i := range pids {
		pids[i] = pid.PID{Host: "host", UniqID: fmt.Sprintf("%d", i)}.Precomputed()
		_ = topo.Register(pids[i])
	}

	main := pids[0]

	b.ResetTimer()
	for i := 1; i <= b.N; i++ {
		_ = topo.Link(main, pids[i])
	}
}

func BenchmarkGetLinks(b *testing.B) {
	topo := NewTopology(&discardReceiver{}, "local")

	main := pid.PID{Host: "host", UniqID: "main"}.Precomputed()
	_ = topo.Register(main)

	for i := 0; i < 100; i++ {
		linked := pid.PID{Host: "host", UniqID: fmt.Sprintf("linked-%d", i)}.Precomputed()
		_ = topo.Register(linked)
		_ = topo.Link(main, linked)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		topo.GetLinks(main)
	}
}

func BenchmarkWait(b *testing.B) {
	topo := NewTopology(&discardReceiver{}, "local")

	target := pid.PID{Host: "host", UniqID: "target"}.Precomputed()
	_ = topo.Register(target)

	callers := make([]pid.PID, b.N)
	for i := range callers {
		callers[i] = pid.PID{Host: "host", UniqID: fmt.Sprintf("caller-%d", i)}.Precomputed()
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = topo.Monitor(callers[i], target)
	}
}

func BenchmarkConcurrentRegisterRemove(b *testing.B) {
	topo := NewTopology(&discardReceiver{}, "local")
	numGoroutines := runtime.GOMAXPROCS(0)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			pid := pid.PID{Host: "host", UniqID: fmt.Sprintf("%d-%d", i, numGoroutines)}.Precomputed()
			_ = topo.Register(pid)
			topo.Remove(pid)
			i++
		}
	})
}

func BenchmarkConcurrentLink(b *testing.B) {
	topo := NewTopology(&discardReceiver{}, "local")

	main := pid.PID{Host: "host", UniqID: "main"}.Precomputed()
	_ = topo.Register(main)

	pids := make([]pid.PID, b.N)
	for i := range pids {
		pids[i] = pid.PID{Host: "host", UniqID: fmt.Sprintf("%d", i)}.Precomputed()
		_ = topo.Register(pids[i])
	}

	var counter int64
	var mu sync.Mutex

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			mu.Lock()
			idx := counter
			counter++
			mu.Unlock()
			if idx < int64(len(pids)) {
				_ = topo.Link(main, pids[idx])
			}
		}
	})
}

func BenchmarkPIDString(b *testing.B) {
	pid := pid.PID{Host: "host", UniqID: "12345"}.Precomputed()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = pid.String()
	}
}

func BenchmarkPIDStringNotPrecomputed(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pid := pid.PID{Host: "host", UniqID: "12345"}
		_ = pid.String()
	}
}

func BenchmarkParsePID(b *testing.B) {
	s := "{host|12345}"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = pid.ParsePID(s)
	}
}

func BenchmarkManyProcessesWithLinks(b *testing.B) {
	for _, numProcesses := range []int{1000, 10000, 100000} {
		b.Run(fmt.Sprintf("n=%d", numProcesses), func(b *testing.B) {
			if testing.Short() && numProcesses >= 100000 {
				b.Skip("skipping large stress benchmark in short mode")
			}
			b.ResetTimer()
			for n := 0; n < b.N; n++ {
				b.StopTimer()
				topo := NewTopology(&discardReceiver{}, "local")
				pids := make([]pid.PID, numProcesses)
				for i := range pids {
					pids[i] = pid.PID{Host: "host", UniqID: fmt.Sprintf("%d", i)}.Precomputed()
				}
				b.StartTimer()

				// Register all
				for i := 0; i < numProcesses; i++ {
					_ = topo.Register(pids[i])
				}

				// Link in a chain (each process links to next)
				for i := 0; i < numProcesses-1; i++ {
					_ = topo.Link(pids[i], pids[i+1])
				}

				// Remove from start (triggers link cleanup)
				for i := 0; i < numProcesses; i++ {
					topo.Remove(pids[i])
				}
			}
		})
	}
}

func BenchmarkMapLookup(b *testing.B) {
	m := make(map[string]*processState)
	for i := 0; i < 100000; i++ {
		key := fmt.Sprintf("{host|%d}", i)
		m[key] = &processState{
			watchers: make(map[string]pid.PID),
			links:    make(map[string]pid.PID),
		}
	}

	key := "{host|50000}"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = m[key]
	}
}

func BenchmarkMapDelete(b *testing.B) {
	for _, size := range []int{1000, 10000, 100000} {
		b.Run(fmt.Sprintf("size=%d", size), func(b *testing.B) {
			b.ResetTimer()
			for n := 0; n < b.N; n++ {
				b.StopTimer()
				m := make(map[string]*processState)
				keys := make([]string, size)
				for i := 0; i < size; i++ {
					keys[i] = fmt.Sprintf("{host|%d}", i)
					m[keys[i]] = &processState{
						watchers: make(map[string]pid.PID),
						links:    make(map[string]pid.PID),
					}
				}
				b.StartTimer()

				for _, k := range keys {
					delete(m, k)
				}
			}
		})
	}
}

func BenchmarkParallelRegisterRemove(b *testing.B) {
	for _, numGoroutines := range []int{1, 4, 16, 32, 64} {
		b.Run(fmt.Sprintf("goroutines=%d", numGoroutines), func(b *testing.B) {
			topo := NewTopology(&discardReceiver{}, "local")
			opsPerGoroutine := b.N / numGoroutines
			if opsPerGoroutine < 1 {
				opsPerGoroutine = 1
			}

			b.ResetTimer()
			var wg sync.WaitGroup
			for g := 0; g < numGoroutines; g++ {
				wg.Add(1)
				go func(gid int) {
					defer wg.Done()
					for i := 0; i < opsPerGoroutine; i++ {
						pid := pid.PID{Host: "host", UniqID: fmt.Sprintf("%d-%d", gid, i)}.Precomputed()
						_ = topo.Register(pid)
						topo.Remove(pid)
					}
				}(g)
			}
			wg.Wait()
		})
	}
}

func BenchmarkHighContention(b *testing.B) {
	topo := NewTopology(&discardReceiver{}, "local")

	// Pre-register some processes
	const numProcesses = 10000
	pids := make([]pid.PID, numProcesses)
	for i := range pids {
		pids[i] = pid.PID{Host: "host", UniqID: fmt.Sprintf("%d", i)}.Precomputed()
		_ = topo.Register(pids[i])
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		var i int
		for pb.Next() {
			idx := i % numProcesses
			// Simulate typical operations
			switch i % 4 {
			case 0:
				_ = topo.Register(pids[idx])
			case 1:
				_ = topo.Monitor(pids[(idx+1)%numProcesses], pids[idx])
			case 2:
				_ = topo.Link(pids[idx], pids[(idx+1)%numProcesses])
			case 3:
				_ = topo.Demonitor(pids[(idx+1)%numProcesses], pids[idx])
			}
			i++
		}
	})
}

func BenchmarkWriteHeavy(b *testing.B) {
	b.RunParallel(func(pb *testing.PB) {
		topo := NewTopology(&discardReceiver{}, "local")
		var i int
		for pb.Next() {
			pid := pid.PID{Host: "host", UniqID: fmt.Sprintf("%d", i)}.Precomputed()
			_ = topo.Register(pid)
			topo.Remove(pid)
			i++
		}
	})
}

// Benchmarks comparing implementations

func BenchmarkOriginalRegister(b *testing.B) {
	topo := NewTopology(&discardReceiver{}, "local")
	pids := make([]pid.PID, b.N)
	for i := range pids {
		pids[i] = pid.PID{Host: "host", UniqID: fmt.Sprintf("%d", i)}.Precomputed()
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = topo.Register(pids[i])
	}
}

func BenchmarkOriginalParallel(b *testing.B) {
	topo := NewTopology(&discardReceiver{}, "local")

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		var i int
		for pb.Next() {
			pid := pid.PID{Host: "host", UniqID: fmt.Sprintf("%d", i)}.Precomputed()
			_ = topo.Register(pid)
			topo.Remove(pid)
			i++
		}
	})
}

func BenchmarkOriginal100k(b *testing.B) {
	if testing.Short() {
		b.Skip("skipping stress benchmark in short mode")
	}
	const numProcesses = 100000

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		topo := NewTopology(&discardReceiver{}, "local")
		pids := make([]pid.PID, numProcesses)

		for i := 0; i < numProcesses; i++ {
			pids[i] = pid.PID{Host: "host", UniqID: fmt.Sprintf("%d", i)}.Precomputed()
			_ = topo.Register(pids[i])
		}

		for i := 0; i < numProcesses; i++ {
			topo.Remove(pids[i])
		}
	}
}

func BenchmarkOriginal100kParallel(b *testing.B) {
	if testing.Short() {
		b.Skip("skipping stress benchmark in short mode")
	}
	const numProcesses = 100000

	for n := 0; n < b.N; n++ {
		topo := NewTopology(&discardReceiver{}, "local")
		pids := make([]pid.PID, numProcesses)
		for i := range pids {
			pids[i] = pid.PID{Host: "host", UniqID: fmt.Sprintf("%d", i)}.Precomputed()
		}

		b.StopTimer()
		b.StartTimer()

		var wg sync.WaitGroup
		numWorkers := runtime.GOMAXPROCS(0)
		perWorker := numProcesses / numWorkers

		for w := 0; w < numWorkers; w++ {
			wg.Add(1)
			start := w * perWorker
			end := start + perWorker
			go func() {
				defer wg.Done()
				for i := start; i < end; i++ {
					_ = topo.Register(pids[i])
				}
			}()
		}
		wg.Wait()

		for w := 0; w < numWorkers; w++ {
			wg.Add(1)
			start := w * perWorker
			end := start + perWorker
			go func() {
				defer wg.Done()
				for i := start; i < end; i++ {
					topo.Remove(pids[i])
				}
			}()
		}
		wg.Wait()
	}
}

func BenchmarkRemoteWait(b *testing.B) {
	topo := NewTopology(&discardReceiver{}, "local")
	localPID := pid.PID{Host: "host", UniqID: "local"}.Precomputed()
	_ = topo.Register(localPID)

	remotePIDs := make([]pid.PID, b.N)
	for i := range remotePIDs {
		remotePIDs[i] = pid.PID{Node: "remote", Host: "host", UniqID: fmt.Sprintf("r%d", i)}.Precomputed()
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = topo.Monitor(localPID, remotePIDs[i])
	}
}

func BenchmarkHandleNodeExit(b *testing.B) {
	b.Run("small", func(b *testing.B) {
		benchHandleNodeExit(b, 10)
	})
	b.Run("medium", func(b *testing.B) {
		benchHandleNodeExit(b, 100)
	})
	b.Run("large", func(b *testing.B) {
		if testing.Short() {
			b.Skip("skipping large benchmark in short mode")
		}
		benchHandleNodeExit(b, 1000)
	})
}

func benchHandleNodeExit(b *testing.B, numWatchers int) {
	for n := 0; n < b.N; n++ {
		b.StopTimer()
		topo := NewTopology(&discardReceiver{}, "local")

		localPIDs := make([]pid.PID, numWatchers)
		for i := range localPIDs {
			localPIDs[i] = pid.PID{Host: "host", UniqID: fmt.Sprintf("p%d", i)}.Precomputed()
			_ = topo.Register(localPIDs[i])
		}

		remotePID := pid.PID{Node: "remote", Host: "host", UniqID: "target"}.Precomputed()
		for _, pid := range localPIDs {
			_ = topo.Monitor(pid, remotePID)
		}
		b.StartTimer()

		topo.HandleNodeExit("remote", errors.New("disconnected"))
	}
}

func BenchmarkConcurrentRemoteWatch(b *testing.B) {
	if testing.Short() {
		b.Skip("skipping stress benchmark in short mode")
	}

	topo := NewTopology(&discardReceiver{}, "local")

	const numProcesses = 1000
	localPIDs := make([]pid.PID, numProcesses)
	for i := range localPIDs {
		localPIDs[i] = pid.PID{Host: "host", UniqID: fmt.Sprintf("p%d", i)}.Precomputed()
		_ = topo.Register(localPIDs[i])
	}

	remotePID := pid.PID{Node: "remote", Host: "host", UniqID: "target"}.Precomputed()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			idx := i % numProcesses
			_ = topo.Monitor(localPIDs[idx], remotePID)
			_ = topo.Demonitor(localPIDs[idx], remotePID)
			i++
		}
	})
}

func BenchmarkConcurrentNodeExit(b *testing.B) {
	if testing.Short() {
		b.Skip("skipping stress benchmark in short mode")
	}

	const numProcesses = 100
	const numNodes = 10

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		b.StopTimer()
		topo := NewTopology(&discardReceiver{}, "local")

		localPIDs := make([]pid.PID, numProcesses)
		for i := range localPIDs {
			localPIDs[i] = pid.PID{Host: "host", UniqID: fmt.Sprintf("p%d", i)}.Precomputed()
			_ = topo.Register(localPIDs[i])
		}

		for i := 0; i < numProcesses; i++ {
			nodeID := fmt.Sprintf("node%d", i%numNodes)
			remotePID := pid.PID{Node: nodeID, Host: "host", UniqID: fmt.Sprintf("r%d", i)}.Precomputed()
			_ = topo.Monitor(localPIDs[i], remotePID)
		}
		b.StartTimer()

		var wg sync.WaitGroup
		for i := 0; i < numNodes; i++ {
			wg.Add(1)
			nodeID := fmt.Sprintf("node%d", i)
			go func(nid string) {
				defer wg.Done()
				topo.HandleNodeExit(nid, errors.New("exit"))
			}(nodeID)
		}
		wg.Wait()
	}
}
