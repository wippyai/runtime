package topology

import (
	"sync"
	"testing"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/runtime"
)

func BenchmarkTopology_Register(b *testing.B) {
	upstream := newMockUpstream()
	topo := NewTopology(upstream, "local")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p := pid.PID{Host: "host", UniqID: string(rune(i % 10000))}
		p = p.Precomputed()
		_ = topo.Register(p)
	}
}

func BenchmarkTopology_RegisterParallel(b *testing.B) {
	upstream := newMockUpstream()
	topo := NewTopology(upstream, "local")

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			p := pid.PID{Host: "host", UniqID: string(rune(i % 10000))}
			p = p.Precomputed()
			_ = topo.Register(p)
			i++
		}
	})
}

func BenchmarkTopology_Monitor(b *testing.B) {
	upstream := newMockUpstream()
	topo := NewTopology(upstream, "local")

	// Pre-register processes
	pids := make([]pid.PID, 1000)
	for i := range pids {
		pids[i] = pid.PID{Host: "host", UniqID: string(rune(i))}
		pids[i] = pids[i].Precomputed()
		_ = topo.Register(pids[i])
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		caller := pids[i%len(pids)]
		target := pids[(i+1)%len(pids)]
		_ = topo.Monitor(caller, target)
	}
}

func BenchmarkTopology_MonitorParallel(b *testing.B) {
	upstream := newMockUpstream()
	topo := NewTopology(upstream, "local")

	// Pre-register processes
	pids := make([]pid.PID, 1000)
	for i := range pids {
		pids[i] = pid.PID{Host: "host", UniqID: string(rune(i))}
		pids[i] = pids[i].Precomputed()
		_ = topo.Register(pids[i])
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			caller := pids[i%len(pids)]
			target := pids[(i+1)%len(pids)]
			_ = topo.Monitor(caller, target)
			i++
		}
	})
}

func BenchmarkTopology_Link(b *testing.B) {
	upstream := newMockUpstream()
	topo := NewTopology(upstream, "local")

	// Pre-register processes
	pids := make([]pid.PID, 1000)
	for i := range pids {
		pids[i] = pid.PID{Host: "host", UniqID: string(rune(i))}
		pids[i] = pids[i].Precomputed()
		_ = topo.Register(pids[i])
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		from := pids[i%len(pids)]
		to := pids[(i+1)%len(pids)]
		_ = topo.Link(from, to)
	}
}

func BenchmarkTopology_LinkParallel(b *testing.B) {
	upstream := newMockUpstream()
	topo := NewTopology(upstream, "local")

	// Pre-register processes
	pids := make([]pid.PID, 1000)
	for i := range pids {
		pids[i] = pid.PID{Host: "host", UniqID: string(rune(i))}
		pids[i] = pids[i].Precomputed()
		_ = topo.Register(pids[i])
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			from := pids[i%len(pids)]
			to := pids[(i+1)%len(pids)]
			_ = topo.Link(from, to)
			i++
		}
	})
}

func BenchmarkTopology_Complete(b *testing.B) {
	b.Run("NoWatchers", func(b *testing.B) {
		upstream := newMockUpstream()
		topo := NewTopology(upstream, "local")

		// Pre-register processes
		pids := make([]pid.PID, b.N)
		for i := range pids {
			pids[i] = pid.PID{Host: "host", UniqID: string(rune(i))}
			pids[i] = pids[i].Precomputed()
			_ = topo.Register(pids[i])
		}

		result := &runtime.Result{Value: payload.New("done")}
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			topo.Complete(pids[i], result)
		}
	})

	b.Run("WithWatchers", func(b *testing.B) {
		upstream := newMockUpstream()
		topo := NewTopology(upstream, "local")

		watcher := pid.PID{Host: "host", UniqID: "watcher"}
		watcher = watcher.Precomputed()
		_ = topo.Register(watcher)

		// Pre-register targets with monitors
		targets := make([]pid.PID, b.N)
		for i := range targets {
			targets[i] = pid.PID{Host: "host", UniqID: string(rune(i))}
			targets[i] = targets[i].Precomputed()
			_ = topo.Register(targets[i])
			_ = topo.Monitor(watcher, targets[i])
		}

		result := &runtime.Result{Value: payload.New("done")}
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			topo.Complete(targets[i], result)
		}
	})
}

func BenchmarkTopology_GetLinks(b *testing.B) {
	upstream := newMockUpstream()
	topo := NewTopology(upstream, "local")

	// Create a process with many links
	main := pid.PID{Host: "host", UniqID: "main"}
	main = main.Precomputed()
	_ = topo.Register(main)

	for i := 0; i < 100; i++ {
		linked := pid.PID{Host: "host", UniqID: string(rune(i))}
		linked = linked.Precomputed()
		_ = topo.Register(linked)
		_ = topo.Link(main, linked)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = topo.GetLinks(main)
	}
}

func BenchmarkTopology_HighContention(b *testing.B) {
	upstream := newMockUpstream()
	topo := NewTopology(upstream, "local")

	// All operations on the same two PIDs
	pid1 := pid.PID{Host: "host", UniqID: "1"}
	pid1 = pid1.Precomputed()
	pid2 := pid.PID{Host: "host", UniqID: "2"}
	pid2 = pid2.Precomputed()

	_ = topo.Register(pid1)
	_ = topo.Register(pid2)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = topo.Monitor(pid1, pid2)
			_ = topo.Demonitor(pid1, pid2)
		}
	})
}

func BenchmarkPIDRegistry_Register(b *testing.B) {
	reg := NewPIDRegistry()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p := pid.PID{Host: "host", UniqID: string(rune(i % 10000))}
		name := string(rune(i % 10000))
		_, _ = reg.Register(name, p)
	}
}

func BenchmarkPIDRegistry_Lookup(b *testing.B) {
	reg := NewPIDRegistry()

	// Pre-register entries
	for i := 0; i < 1000; i++ {
		p := pid.PID{Host: "host", UniqID: string(rune(i))}
		name := string(rune(i))
		_, _ = reg.Register(name, p)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		name := string(rune(i % 1000))
		_, _ = reg.Lookup(name)
	}
}

func BenchmarkPIDRegistry_LookupParallel(b *testing.B) {
	reg := NewPIDRegistry()

	// Pre-register entries
	for i := 0; i < 1000; i++ {
		p := pid.PID{Host: "host", UniqID: string(rune(i))}
		name := string(rune(i))
		_, _ = reg.Register(name, p)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			name := string(rune(i % 1000))
			_, _ = reg.Lookup(name)
			i++
		}
	})
}

func BenchmarkTopology_MixedWorkload(b *testing.B) {
	upstream := newMockUpstream()
	topo := NewTopology(upstream, "local")

	// Pre-register processes
	pids := make([]pid.PID, 100)
	for i := range pids {
		pids[i] = pid.PID{Host: "host", UniqID: string(rune(i))}
		pids[i] = pids[i].Precomputed()
		_ = topo.Register(pids[i])
	}

	var wg sync.WaitGroup
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		wg.Add(4)
		idx := i % len(pids)

		go func() {
			defer wg.Done()
			_ = topo.Monitor(pids[idx], pids[(idx+1)%len(pids)])
		}()
		go func() {
			defer wg.Done()
			_ = topo.Link(pids[idx], pids[(idx+2)%len(pids)])
		}()
		go func() {
			defer wg.Done()
			_ = topo.GetLinks(pids[idx])
		}()
		go func() {
			defer wg.Done()
			_ = topo.Demonitor(pids[idx], pids[(idx+1)%len(pids)])
		}()

		wg.Wait()
	}
}
