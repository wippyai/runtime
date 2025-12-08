package topology

import (
	"fmt"
	"runtime"
	"sync"
	"testing"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/relay"
	runtimeapi "github.com/wippyai/runtime/api/runtime"
)

type discardReceiver struct{}

func (d *discardReceiver) Send(*relay.Package) error { return nil }

func BenchmarkRegister(b *testing.B) {
	topo := NewTopology(&discardReceiver{}, &discardReceiver{}, "local")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pid := relay.PID{Host: "host", UniqID: fmt.Sprintf("%d", i)}.Precomputed()
		_ = topo.Register(pid)
	}
}

func BenchmarkRegisterPrecomputed(b *testing.B) {
	topo := NewTopology(&discardReceiver{}, &discardReceiver{}, "local")
	pids := make([]relay.PID, b.N)
	for i := range pids {
		pids[i] = relay.PID{Host: "host", UniqID: fmt.Sprintf("%d", i)}.Precomputed()
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = topo.Register(pids[i])
	}
}

func BenchmarkRemove(b *testing.B) {
	topo := NewTopology(&discardReceiver{}, &discardReceiver{}, "local")
	pids := make([]relay.PID, b.N)
	for i := range pids {
		pids[i] = relay.PID{Host: "host", UniqID: fmt.Sprintf("%d", i)}.Precomputed()
		_ = topo.Register(pids[i])
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		topo.Remove(pids[i])
	}
}

func BenchmarkRegisterAndRemove(b *testing.B) {
	topo := NewTopology(&discardReceiver{}, &discardReceiver{}, "local")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pid := relay.PID{Host: "host", UniqID: fmt.Sprintf("%d", i)}.Precomputed()
		_ = topo.Register(pid)
		topo.Remove(pid)
	}
}

func BenchmarkRegisterRemove100k(b *testing.B) {
	const numProcesses = 100000

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		topo := NewTopology(&discardReceiver{}, &discardReceiver{}, "local")
		pids := make([]relay.PID, numProcesses)

		// Register all
		for i := 0; i < numProcesses; i++ {
			pids[i] = relay.PID{Host: "host", UniqID: fmt.Sprintf("%d", i)}.Precomputed()
			_ = topo.Register(pids[i])
		}

		// Remove all
		for i := 0; i < numProcesses; i++ {
			topo.Remove(pids[i])
		}
	}
}

func BenchmarkNotify(b *testing.B) {
	topo := NewTopology(&discardReceiver{}, &discardReceiver{}, "local")

	monitored := relay.PID{Host: "host", UniqID: "monitored"}.Precomputed()
	_ = topo.Register(monitored)

	watchers := make([]relay.PID, 10)
	for i := range watchers {
		watchers[i] = relay.PID{Host: "host", UniqID: fmt.Sprintf("watcher-%d", i)}.Precomputed()
		_ = topo.Register(watchers[i])
		_ = topo.Wait(watchers[i], monitored)
	}

	result := &runtimeapi.Result{Value: payload.New("test")}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		topo.Notify(monitored, result)
	}
}

func BenchmarkNotifyWithLinks(b *testing.B) {
	topo := NewTopology(&discardReceiver{}, &discardReceiver{}, "local")

	main := relay.PID{Host: "host", UniqID: "main"}.Precomputed()
	_ = topo.Register(main)

	linked := make([]relay.PID, 10)
	for i := range linked {
		linked[i] = relay.PID{Host: "host", UniqID: fmt.Sprintf("linked-%d", i)}.Precomputed()
		_ = topo.Register(linked[i])
		_ = topo.Link(main, linked[i])
	}

	result := &runtimeapi.Result{Value: payload.New("test"), Error: fmt.Errorf("crash")}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		topo.Notify(main, result)
	}
}

func BenchmarkLink(b *testing.B) {
	topo := NewTopology(&discardReceiver{}, &discardReceiver{}, "local")

	pids := make([]relay.PID, b.N+1)
	for i := range pids {
		pids[i] = relay.PID{Host: "host", UniqID: fmt.Sprintf("%d", i)}.Precomputed()
		_ = topo.Register(pids[i])
	}

	main := pids[0]

	b.ResetTimer()
	for i := 1; i <= b.N; i++ {
		_ = topo.Link(main, pids[i])
	}
}

func BenchmarkGetLinks(b *testing.B) {
	topo := NewTopology(&discardReceiver{}, &discardReceiver{}, "local")

	main := relay.PID{Host: "host", UniqID: "main"}.Precomputed()
	_ = topo.Register(main)

	for i := 0; i < 100; i++ {
		linked := relay.PID{Host: "host", UniqID: fmt.Sprintf("linked-%d", i)}.Precomputed()
		_ = topo.Register(linked)
		_ = topo.Link(main, linked)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		topo.GetLinks(main)
	}
}

func BenchmarkWait(b *testing.B) {
	topo := NewTopology(&discardReceiver{}, &discardReceiver{}, "local")

	target := relay.PID{Host: "host", UniqID: "target"}.Precomputed()
	_ = topo.Register(target)

	callers := make([]relay.PID, b.N)
	for i := range callers {
		callers[i] = relay.PID{Host: "host", UniqID: fmt.Sprintf("caller-%d", i)}.Precomputed()
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = topo.Wait(callers[i], target)
	}
}

func BenchmarkConcurrentRegisterRemove(b *testing.B) {
	topo := NewTopology(&discardReceiver{}, &discardReceiver{}, "local")
	numGoroutines := runtime.GOMAXPROCS(0)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			pid := relay.PID{Host: "host", UniqID: fmt.Sprintf("%d-%d", i, numGoroutines)}.Precomputed()
			_ = topo.Register(pid)
			topo.Remove(pid)
			i++
		}
	})
}

func BenchmarkConcurrentLink(b *testing.B) {
	topo := NewTopology(&discardReceiver{}, &discardReceiver{}, "local")

	main := relay.PID{Host: "host", UniqID: "main"}.Precomputed()
	_ = topo.Register(main)

	pids := make([]relay.PID, b.N)
	for i := range pids {
		pids[i] = relay.PID{Host: "host", UniqID: fmt.Sprintf("%d", i)}.Precomputed()
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
	pid := relay.PID{Host: "host", UniqID: "12345"}.Precomputed()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = pid.String()
	}
}

func BenchmarkPIDStringNotPrecomputed(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pid := relay.PID{Host: "host", UniqID: "12345"}
		_ = pid.String()
	}
}

func BenchmarkParsePID(b *testing.B) {
	s := "{host|12345}"
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = relay.ParsePID(s)
	}
}

func BenchmarkManyProcessesWithLinks(b *testing.B) {
	for _, numProcesses := range []int{1000, 10000, 100000} {
		b.Run(fmt.Sprintf("n=%d", numProcesses), func(b *testing.B) {
			b.ResetTimer()
			for n := 0; n < b.N; n++ {
				b.StopTimer()
				topo := NewTopology(&discardReceiver{}, &discardReceiver{}, "local")
				pids := make([]relay.PID, numProcesses)
				for i := range pids {
					pids[i] = relay.PID{Host: "host", UniqID: fmt.Sprintf("%d", i)}.Precomputed()
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
			watchers: make(map[string]bool),
			links:    make(map[string]bool),
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
						watchers: make(map[string]bool),
						links:    make(map[string]bool),
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
			topo := NewTopology(&discardReceiver{}, &discardReceiver{}, "local")
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
						pid := relay.PID{Host: "host", UniqID: fmt.Sprintf("%d-%d", gid, i)}.Precomputed()
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
	topo := NewTopology(&discardReceiver{}, &discardReceiver{}, "local")

	// Pre-register some processes
	const numProcesses = 10000
	pids := make([]relay.PID, numProcesses)
	for i := range pids {
		pids[i] = relay.PID{Host: "host", UniqID: fmt.Sprintf("%d", i)}.Precomputed()
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
				_ = topo.Wait(pids[(idx+1)%numProcesses], pids[idx])
			case 2:
				_ = topo.Link(pids[idx], pids[(idx+1)%numProcesses])
			case 3:
				_ = topo.Release(pids[(idx+1)%numProcesses], pids[idx])
			}
			i++
		}
	})
}

func BenchmarkWriteHeavy(b *testing.B) {
	b.RunParallel(func(pb *testing.PB) {
		topo := NewTopology(&discardReceiver{}, &discardReceiver{}, "local")
		var i int
		for pb.Next() {
			pid := relay.PID{Host: "host", UniqID: fmt.Sprintf("%d", i)}.Precomputed()
			_ = topo.Register(pid)
			topo.Remove(pid)
			i++
		}
	})
}

// Benchmarks comparing implementations

func BenchmarkOriginalRegister(b *testing.B) {
	topo := NewTopology(&discardReceiver{}, &discardReceiver{}, "local")
	pids := make([]relay.PID, b.N)
	for i := range pids {
		pids[i] = relay.PID{Host: "host", UniqID: fmt.Sprintf("%d", i)}.Precomputed()
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = topo.Register(pids[i])
	}
}

func BenchmarkOriginalParallel(b *testing.B) {
	topo := NewTopology(&discardReceiver{}, &discardReceiver{}, "local")

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		var i int
		for pb.Next() {
			pid := relay.PID{Host: "host", UniqID: fmt.Sprintf("%d", i)}.Precomputed()
			_ = topo.Register(pid)
			topo.Remove(pid)
			i++
		}
	})
}

func BenchmarkOriginal100k(b *testing.B) {
	const numProcesses = 100000

	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		topo := NewTopology(&discardReceiver{}, &discardReceiver{}, "local")
		pids := make([]relay.PID, numProcesses)

		for i := 0; i < numProcesses; i++ {
			pids[i] = relay.PID{Host: "host", UniqID: fmt.Sprintf("%d", i)}.Precomputed()
			_ = topo.Register(pids[i])
		}

		for i := 0; i < numProcesses; i++ {
			topo.Remove(pids[i])
		}
	}
}

func BenchmarkOriginal100kParallel(b *testing.B) {
	const numProcesses = 100000

	for n := 0; n < b.N; n++ {
		topo := NewTopology(&discardReceiver{}, &discardReceiver{}, "local")
		pids := make([]relay.PID, numProcesses)
		for i := range pids {
			pids[i] = relay.PID{Host: "host", UniqID: fmt.Sprintf("%d", i)}.Precomputed()
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
