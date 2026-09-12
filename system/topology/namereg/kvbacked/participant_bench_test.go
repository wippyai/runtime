// SPDX-License-Identifier: MPL-2.0
package kvbacked

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"testing"
	"time"

	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
)

// BenchmarkParticipantSnapshotRoundTrip includes request encoding, receiver
// admission, idempotent enrollment, coherent capture, response encoding/decoding
// and client validation. Relay is in-process and KV standalone: this is neither
// TLS throughput nor 100 running nodes nor Raft/load acceptance.
func BenchmarkParticipantSnapshotRoundTrip(b *testing.B) {
	for _, participants := range []int{10, 100} {
		for _, claims := range []int{0, 1000} {
			b.Run(fmt.Sprintf("participants=%d/claims=%d", participants, claims), func(b *testing.B) {
				ctx := context.Background()
				inventory, engine := newParticipantTestInventory(b, participants+1)
				for i := 0; i < participants; i++ {
					if err := inventory.enroll(ctx, fmt.Sprintf("node-%d", i), "one"); err != nil {
						b.Fatal(err)
					}
				}
				owner := pid.PID{Node: "node-0", Host: "process", UniqID: "owner"}
				for i := 0; i < claims; i++ {
					name := fmt.Sprintf("claim-%06d", i)
					body, err := encode(activeValue{Name: name, PID: owner.String(), Strong: true})
					if err != nil {
						b.Fatal(err)
					}
					if _, err = engine.Set(activeKey(name), body); err != nil {
						b.Fatal(err)
					}
				}
				limits := participantSnapshotLimits{MaxEntries: claims + 1, MaxValueBytes: 4 << 20}
				mesh := &participantTestMesh{nodes: make(map[pid.NodeID]relay.ContextSender)}
				authority, err := newParticipantAuthority(ctx, "authority", inventory, engine, limits, 1)
				if err != nil {
					b.Fatal(err)
				}
				receiver, err := newParticipantReceiver(authority, mesh, 8<<20, claims+1, time.Second)
				if err != nil {
					b.Fatal(err)
				}
				client, err := newParticipantClient(ctx, "node-0", "one", func(context.Context) (pid.NodeID, error) { return "authority", nil }, mesh, 8<<20, claims+1, time.Second)
				if err != nil {
					b.Fatal(err)
				}
				mesh.nodes["authority"], mesh.nodes["node-0"] = receiver, client
				b.Cleanup(func() {
					if err := client.stop(ctx); err != nil {
						b.Error(err)
					}
					if err := receiver.stop(ctx); err != nil {
						b.Error(err)
					}
				})
				busy := 0
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					var snapshot *participantSnapshot
					for attempt := 0; ; attempt++ {
						snapshot, err = inventory.captureSnapshot(ctx, client, "node-0", "one", limits)
						if !errors.Is(err, errParticipantAuthorityBusy) || attempt >= 1000 {
							break
						}
						busy++
						runtime.Gosched()
					}
					if err != nil {
						b.Fatal(err)
					}
					if len(snapshot.Entries) != claims+1 {
						b.Fatal("incomplete snapshot")
					}
				}
				b.StopTimer()
				b.ReportMetric(float64(busy)/float64(b.N), "busy/op")

			})
		}
	}
}
