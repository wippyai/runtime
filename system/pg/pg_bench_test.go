// SPDX-License-Identifier: MPL-2.0

package pg

import (
	"context"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	pgapi "github.com/wippyai/runtime/api/pg"
	"go.uber.org/zap"
)

func startBenchService(b *testing.B) *Service {
	b.Helper()
	return startBenchServiceConfig(b, nil)
}

func startBenchServiceConfig(b *testing.B, cfg *pgapi.Config) *Service {
	b.Helper()
	router := newMockRouter()
	topo := newMockTopology()
	logger := zap.NewNop()
	svc := NewService(logger, "pg", cfg, router, topo, nil, nil, "local-node", nil, nil, nil)
	if _, err := svc.Start(context.Background()); err != nil {
		b.Fatalf("Start: %v", err)
	}
	b.Cleanup(func() {
		_ = svc.Stop(context.Background())
	})
	time.Sleep(10 * time.Millisecond)
	return svc
}

func BenchmarkPGJoinLeave_Basal(b *testing.B) {
	svc := startBenchService(b)
	p := mkPID("h", "1")
	if err := svc.Join("warm", p); err != nil {
		b.Fatalf("warm: %v", err)
	}
	if err := svc.Leave("warm", p); err != nil {
		b.Fatalf("warm leave: %v", err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := svc.Join("g", p); err != nil {
			b.Fatalf("Join: %v", err)
		}
		if err := svc.Leave("g", p); err != nil {
			b.Fatalf("Leave: %v", err)
		}
	}
}

func BenchmarkPGJoinLeave_ManyGroups(b *testing.B) {
	for _, N := range []int{100, 1000, 10000, 100000} {
		b.Run("N="+strconv.Itoa(N), func(b *testing.B) {
			svc := startBenchService(b)
			seedGroups := make([]string, N)
			for i := 0; i < N; i++ {
				seedGroups[i] = "seed-" + strconv.Itoa(i)
			}
			seed := mkPID("seed", "0")
			if err := svc.JoinGroups(seedGroups, seed); err != nil {
				b.Fatalf("seed JoinGroups: %v", err)
			}
			hot := mkPID("hot", "0")
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := svc.Join("hot", hot); err != nil {
					b.Fatalf("Join: %v", err)
				}
				if err := svc.Leave("hot", hot); err != nil {
					b.Fatalf("Leave: %v", err)
				}
			}
		})
	}
}

func BenchmarkPGSnapshotGroup(b *testing.B) {
	for _, M := range []int{1, 10, 100, 1000, 10000} {
		b.Run("M="+strconv.Itoa(M), func(b *testing.B) {
			s := newState()
			for i := 0; i < M; i++ {
				s.joinLocal("hot", mkPID("h", strconv.Itoa(i)))
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = s.snapshotGroup("hot")
			}
		})
	}
}

func BenchmarkPGJoin_Parallel(b *testing.B) {
	cfg := &pgapi.Config{}
	cfg.InitDefaults()
	cfg.ActionQueueSize = 4096
	cfg.ActionQueueMaxSize = 16384
	svc := startBenchServiceConfig(b, cfg)

	b.ReportAllocs()
	var counter uint64
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			i := atomic.AddUint64(&counter, 1)
			p := mkPID("h", strconv.FormatUint(i, 10))
			if err := svc.Join("g", p); err != nil {
				b.Fatalf("Join: %v", err)
			}
			if err := svc.Leave("g", p); err != nil {
				b.Fatalf("Leave: %v", err)
			}
		}
	})
}

func BenchmarkPGJoinGroups_Batch(b *testing.B) {
	for _, N := range []int{10, 100, 1000} {
		b.Run("N="+strconv.Itoa(N), func(b *testing.B) {
			svc := startBenchService(b)
			groups := make([]string, N)
			for i := 0; i < N; i++ {
				groups[i] = "g-" + strconv.Itoa(i)
			}
			p := mkPID("h", "0")
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := svc.JoinGroups(groups, p); err != nil {
					b.Fatalf("JoinGroups: %v", err)
				}
				if err := svc.LeaveGroups(groups, p); err != nil {
					b.Fatalf("LeaveGroups: %v", err)
				}
			}
		})
	}
}
