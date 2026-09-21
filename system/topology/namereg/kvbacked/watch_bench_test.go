// SPDX-License-Identifier: MPL-2.0

package kvbacked

// These benchmarks exercise the in-process reconciler event path. They do not
// include a raft transport, serialization, or a real network. Their purpose is
// to make the pending-scan fanout visible when an ACK/NACK event calls
// reconcileAllPending.

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wippyai/runtime/api/pid"
	kvapi "github.com/wippyai/runtime/api/store/kv"
	"github.com/wippyai/runtime/system/eventbus"
	systemkv "github.com/wippyai/runtime/system/kv"
)

// countingEngine records the operations made by the reconciler while retaining
// the normal in-memory Service implementation and its immutable snapshots.
type countingEngine struct {
	kvapi.Engine
	reader kvapi.LocalSnapshotReader
	gets   atomic.Uint64
	scans  atomic.Uint64
	txns   atomic.Uint64
	snaps  atomic.Uint64
}

func (e *countingEngine) Get(key string) (kvapi.Entry, error) {
	e.gets.Add(1)
	return e.Engine.Get(key)
}

func (e *countingEngine) Scan(prefix string, fn func(kvapi.Entry) bool) error {
	e.scans.Add(1)
	return e.Engine.Scan(prefix, fn)
}

func (e *countingEngine) Txn(ops []kvapi.TxnOp) (bool, error) {
	e.txns.Add(1)
	return e.Engine.Txn(ops)
}

func (e *countingEngine) ReadLocalSnapshot(keys []string) (map[string]kvapi.Entry, uint64, error) {
	e.snaps.Add(1)
	return e.reader.ReadLocalSnapshot(keys)
}

func (e *countingEngine) resetCounts() {
	e.gets.Store(0)
	e.scans.Store(0)
	e.txns.Store(0)
	e.snaps.Store(0)
}

// BenchmarkStrongWatchVoteBurst measures the work caused by a burst of
// synthetic ACK watch events. Each event is fed through handleWatchEvent after
// a pending set is installed. The event itself is synthetic so the benchmark
// isolates event handling; one required ACK stays absent, so no claim is
// promoted and no write is timed.
//
// Required participant counts are 3 and 5; pending claim counts are 1, 16,
// and 64. In the current reconciler, every event scans all pending names, then
// each pending reads N reject keys and N required ACK keys while checking
// whether promotion is possible.
func BenchmarkStrongWatchVoteBurst(b *testing.B) {
	for _, participants := range []int{3, 5} {
		for _, pending := range []int{1, 16, 64} {
			name := fmt.Sprintf("participants=%d/pending=%d", participants, pending)
			b.Run(name, func(b *testing.B) {
				runStrongWatchVoteBurst(b, participants, pending, 0)
			})
		}
	}
}

// BenchmarkStrongWatchVoteBurstUnrelated holds P=1 and N=3 fixed while
// adding unrelated keys. This isolates the cost of scanning a larger KV
// snapshot from the pending-claim fanout itself.
func BenchmarkStrongWatchVoteBurstUnrelated(b *testing.B) {
	for _, unrelated := range []int{0, 1024} {
		b.Run(fmt.Sprintf("unrelated=%d", unrelated), func(b *testing.B) {
			runStrongWatchVoteBurst(b, 3, 1, unrelated)
		})
	}
}

func runStrongWatchVoteBurst(b *testing.B, participants, pending, unrelated int) {
	b.Helper()
	base := systemkv.NewService("watch-bench", eventbus.NewBus(), nil)
	if _, err := base.Start(context.Background()); err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = base.Stop(context.Background()) })

	engine := &countingEngine{Engine: base, reader: base}
	reg := NewService(engine, "observer", nil, nil)
	b.Cleanup(func() {
		reg.strong.mu.Lock()
		for claim, timer := range reg.strong.timers {
			delete(reg.strong.timers, claim)
			timer.Stop()
		}
		reg.strong.mu.Unlock()
	})
	required := make([]pid.NodeID, participants)
	for i := range required {
		required[i] = fmt.Sprintf("node-%d", i)
	}
	reg.ConfigureStrong(StrongDeps{
		Membership: func() []pid.NodeID { return required },
		IsLeader:   func() bool { return true },
		Deadline:   time.Hour,
	})

	for i := 0; i < unrelated; i++ {
		if _, err := base.Set(fmt.Sprintf("unrelated:%d", i), []byte("x")); err != nil {
			b.Fatal(err)
		}
	}
	events := make([]kvapi.WatchEvent, 0, pending*participants)
	for i := 0; i < pending; i++ {
		claim := fmt.Sprintf("watch-bench-%d", i)
		owner := pid.PID{Node: "owner", Host: "h", UniqID: fmt.Sprintf("owner-%d", i)}
		hdr, err := encode(pendingHeader{
			PID: owner.String(), Name: claim,
			RequiredNodes: required, DeadlineUnixNano: time.Now().Add(time.Hour).UnixNano(),
		})
		if err != nil {
			b.Fatal(err)
		}
		if _, err := base.Set(pendingKey(claim), hdr); err != nil {
			b.Fatal(err)
		}
		entry, err := base.Get(pendingKey(claim))
		if err != nil {
			b.Fatal(err)
		}
		for j, node := range required {
			// Keep one required node absent so the pending claim remains
			// in-flight while every event checks the full required set.
			if j == len(required)-1 {
				continue
			}
			if _, err := base.Set(ackKey(claim, entry.Epoch, node), []byte(node)); err != nil {
				b.Fatal(err)
			}
		}
		for _, node := range required {
			events = append(events, kvapi.WatchEvent{Current: &kvapi.Entry{Key: ackKey(claim, entry.Epoch, node)}})
		}
	}

	// Warm timers and decoder paths outside the timed region. The synthetic
	// events leave the pending records untouched, so every operation sees the
	// same P claims.
	for _, ev := range events {
		if err := reg.handleWatchEvent(ev); err != nil {
			b.Fatal(err)
		}
	}
	engine.resetCounts()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, ev := range events {
			if err := reg.handleWatchEvent(ev); err != nil {
				b.Fatal(err)
			}
		}
	}
	b.StopTimer()
	if got := engine.txns.Load(); got != 0 {
		b.Fatalf("incomplete-claim benchmark performed %d KV transactions", got)
	}
	for i := 0; i < pending; i++ {
		if _, err := base.Get(pendingKey(fmt.Sprintf("watch-bench-%d", i))); err != nil {
			b.Fatalf("pending claim %d left the measured workload: %v", i, err)
		}
	}

	eventCount := float64(len(events))
	iterations := float64(b.N)
	b.ReportMetric(eventCount, "events/op")
	b.ReportMetric(float64(engine.scans.Load())/iterations, "scans/op")
	b.ReportMetric(float64(engine.gets.Load())/iterations, "gets/op")
	b.ReportMetric(float64(engine.snaps.Load())/iterations, "snapshots/op")
	b.ReportMetric(float64(engine.txns.Load())/iterations, "txns/op")
	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/(iterations*eventCount), "ns/event")
}

var _ kvapi.LocalSnapshotReader = (*countingEngine)(nil)
