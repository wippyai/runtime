// SPDX-License-Identifier: MPL-2.0
package internode

import (
	"context"
	"sync/atomic"
	"testing"
)

// Measures accounting only: no network, copying, encoding or queue scheduling.
// Comparing shared and independent roots distinguishes aggregate-lock cost from
// reservation allocation; independent roots are a measurement control, not an
// alternative implementation of the process-wide bound.
func BenchmarkOutboundBudget(b *testing.B) {
	for _, shared := range []bool{true, false} {
		name := "shared_100_peers"
		if !shared {
			name = "independent_100_peers"
		}
		b.Run(name, func(b *testing.B) {
			root, err := newOutboundBudget(1<<20, 1<<40)
			if err != nil {
				b.Fatal(err)
			}
			scopes := make([]*outboundBudget, 100)
			for i := range scopes {
				if !shared {
					root, err = newOutboundBudget(1<<20, 1<<40)
					if err != nil {
						b.Fatal(err)
					}
				}
				peer, err := root.child(1<<20, 1<<40)
				if err != nil {
					b.Fatal(err)
				}
				scopes[i], err = peer.child(1<<20, 1<<40)
				if err != nil {
					b.Fatal(err)
				}
			}
			var workers atomic.Uint64
			ctx := context.Background()
			b.ReportAllocs()
			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				scope := scopes[(workers.Add(1)-1)%uint64(len(scopes))]
				for pb.Next() {
					reservation, err := scope.reserve(ctx, 1024, false)
					if err != nil {
						b.Error(err)
						return
					}
					reservation.release()
				}
			})
			b.StopTimer()
			for _, scope := range scopes {
				if scope.root.entries != 0 || scope.root.bytes != 0 {
					b.Fatal("retained capacity after completed workload")
				}
			}
		})
	}
}
