package registry

import (
	"context"
	"fmt"
	"testing"

	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/internal/version"
	historymem "github.com/wippyai/runtime/system/registry/history/memory"
	"github.com/wippyai/runtime/system/registry/topology"
	"go.uber.org/zap"
)

func setupBenchRegistry(b *testing.B, entryCount int) *Reg {
	b.Helper()

	hist := historymem.New()
	runner := &MockRunner{
		RunFunc: func(state registry.State, changes registry.ChangeSet) (registry.State, error) {
			stateMap := make(map[registry.ID]registry.Entry, len(state))
			for _, entry := range state {
				stateMap[entry.ID] = entry
			}
			for _, op := range changes {
				switch op.Kind {
				case registry.EntryCreate, registry.EntryUpdate:
					stateMap[op.Entry.ID] = op.Entry
				case registry.EntryDelete:
					delete(stateMap, op.Entry.ID)
				}
			}
			result := make(registry.State, 0, len(stateMap))
			for _, entry := range stateMap {
				result = append(result, entry)
			}
			return result, nil
		},
	}
	builder := topology.NewStateBuilder(zap.NewNop(), nil)
	reg := NewRegistry(hist, runner, builder, nil, zap.NewNop())

	// Load baseline state
	baseline := make(registry.State, entryCount)
	for i := 0; i < entryCount; i++ {
		baseline[i] = registry.Entry{
			ID:   registry.ID{NS: "bench", Name: fmt.Sprintf("entry-%d", i)},
			Kind: "service",
			Data: payload.NewString(fmt.Sprintf("data-%d", i)),
			Meta: attrs.Bag{
				"index": i,
				"type":  "benchmark",
			},
		}
	}

	err := reg.LoadState(context.Background(), baseline, version.FromParent(nil, 0))
	if err != nil {
		b.Fatalf("failed to load baseline: %v", err)
	}

	return reg
}

// BenchmarkGetEntry measures GetEntry performance with different registry sizes
func BenchmarkGetEntry(b *testing.B) {
	sizes := []int{100, 500, 1000, 5000}

	for _, size := range sizes {
		b.Run(fmt.Sprintf("size=%d", size), func(b *testing.B) {
			reg := setupBenchRegistry(b, size)

			// Lookup entry in the middle
			targetID := registry.ID{NS: "bench", Name: fmt.Sprintf("entry-%d", size/2)}

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				_, err := reg.GetEntry(targetID)
				if err != nil {
					b.Fatalf("GetEntry failed: %v", err)
				}
			}
		})
	}
}

// BenchmarkGetEntry_Miss measures GetEntry performance for non-existent entries
func BenchmarkGetEntry_Miss(b *testing.B) {
	sizes := []int{100, 1000, 5000}

	for _, size := range sizes {
		b.Run(fmt.Sprintf("size=%d", size), func(b *testing.B) {
			reg := setupBenchRegistry(b, size)

			// Lookup non-existent entry
			targetID := registry.NewID("bench", "non-existent")

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				_, _ = reg.GetEntry(targetID)
			}
		})
	}
}

// BenchmarkGetAllEntries measures GetAllEntries performance
func BenchmarkGetAllEntries(b *testing.B) {
	sizes := []int{100, 1000, 5000}

	for _, size := range sizes {
		b.Run(fmt.Sprintf("size=%d", size), func(b *testing.B) {
			reg := setupBenchRegistry(b, size)

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				entries, err := reg.GetAllEntries()
				if err != nil {
					b.Fatalf("GetAllEntries failed: %v", err)
				}
				if len(entries) != size {
					b.Fatalf("expected %d entries, got %d", size, len(entries))
				}
			}
		})
	}
}

// BenchmarkApply measures Apply performance
func BenchmarkApply(b *testing.B) {
	sizes := []int{100, 1000}
	changeSizes := []int{1, 10, 50}

	for _, size := range sizes {
		for _, changeSize := range changeSizes {
			b.Run(fmt.Sprintf("state=%d/changes=%d", size, changeSize), func(b *testing.B) {
				reg := setupBenchRegistry(b, size)
				ctx := context.Background()

				b.ResetTimer()
				b.ReportAllocs()

				for i := 0; i < b.N; i++ {
					changes := make(registry.ChangeSet, changeSize)
					for j := 0; j < changeSize; j++ {
						changes[j] = registry.Operation{
							Kind: registry.EntryUpdate,
							Entry: registry.Entry{
								ID:   registry.ID{NS: "bench", Name: fmt.Sprintf("entry-%d", j%size)},
								Kind: "service",
								Data: payload.NewString(fmt.Sprintf("updated-%d-%d", i, j)),
							},
						}
					}

					_, err := reg.Apply(ctx, changes)
					if err != nil {
						b.Fatalf("Apply failed: %v", err)
					}
				}
			})
		}
	}
}

// BenchmarkLoadState measures LoadState performance
func BenchmarkLoadState(b *testing.B) {
	sizes := []int{100, 500, 1000}

	for _, size := range sizes {
		b.Run(fmt.Sprintf("size=%d", size), func(b *testing.B) {
			hist := historymem.New()
			runner := &MockRunner{
				RunFunc: func(state registry.State, changes registry.ChangeSet) (registry.State, error) {
					stateMap := make(map[registry.ID]registry.Entry, len(state))
					for _, entry := range state {
						stateMap[entry.ID] = entry
					}
					for _, op := range changes {
						switch op.Kind {
						case registry.EntryCreate, registry.EntryUpdate:
							stateMap[op.Entry.ID] = op.Entry
						case registry.EntryDelete:
							delete(stateMap, op.Entry.ID)
						}
					}
					result := make(registry.State, 0, len(stateMap))
					for _, entry := range stateMap {
						result = append(result, entry)
					}
					return result, nil
				},
			}
			builder := topology.NewStateBuilder(zap.NewNop(), nil)

			baseline := make(registry.State, size)
			for i := 0; i < size; i++ {
				baseline[i] = registry.Entry{
					ID:   registry.ID{NS: "bench", Name: fmt.Sprintf("entry-%d", i)},
					Kind: "service",
					Data: payload.NewString(fmt.Sprintf("data-%d", i)),
				}
			}

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				reg := NewRegistry(hist, runner, builder, nil, zap.NewNop())
				err := reg.LoadState(context.Background(), baseline, version.FromParent(nil, 0))
				if err != nil {
					b.Fatalf("LoadState failed: %v", err)
				}
			}
		})
	}
}

// BenchmarkConcurrentGetEntry measures concurrent read performance
func BenchmarkConcurrentGetEntry(b *testing.B) {
	reg := setupBenchRegistry(b, 1000)

	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			targetID := registry.ID{NS: "bench", Name: fmt.Sprintf("entry-%d", i%1000)}
			_, _ = reg.GetEntry(targetID)
			i++
		}
	})
}
