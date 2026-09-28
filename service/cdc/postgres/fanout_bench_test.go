// SPDX-License-Identifier: MPL-2.0

package postgres

import (
	"context"
	"fmt"
	"sync"
	"testing"

	config "github.com/wippyai/runtime/api/service/cdc"
)

// Measures complete source fanout, including queue handoff to every reader.
// It deliberately excludes database/network latency and Lua conversion.
func BenchmarkSourceFanout(b *testing.B) {
	for _, count := range []int{1, 10, 100, 1000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			source := NewSource(SourceOptions{Name: "bench"})
			acks := make(chan struct{}, count)
			streams := make([]config.Stream, count)
			var readers sync.WaitGroup
			for i := range streams {
				stream := source.Subscribe(config.StreamOptions{Buffer: 4, MaxBytes: 4096})
				streams[i] = stream
				readers.Add(1)
				go func() {
					defer readers.Done()
					for range stream.Changes() {
						acks <- struct{}{}
					}
				}()
			}
			b.Cleanup(func() {
				for _, stream := range streams {
					stream.Close()
				}
				readers.Wait()
			})
			change := config.Change{Source: "bench", Op: "insert", Table: "items", After: map[string]any{"id": int64(1)}}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				source.publishChange(context.Background(), change)
				for range count {
					<-acks
				}
			}
			b.StopTimer()
			b.ReportMetric(float64(count), "deliveries/op")
		})
	}
}
