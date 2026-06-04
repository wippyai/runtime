// SPDX-License-Identifier: MPL-2.0

package kv

import (
	"bytes"
	"context"
	"fmt"
	"math/rand"
	"testing"
	"testing/quick"

	kvapi "github.com/wippyai/runtime/api/store/kv"
	"github.com/wippyai/runtime/system/eventbus"
	"go.uber.org/zap"
)

// TestProp_CommandCodecRoundTrip proves encode/decode is lossless for arbitrary
// commands (the wire format the raft FSM applies).
func TestProp_CommandCodecRoundTrip(t *testing.T) {
	f := func(op uint8, key string, value []byte, expect uint64, lease string, ttl int64) bool {
		c := command{
			Op:      opcode(op%9 + 1),
			Key:     key,
			Value:   value,
			Expect:  expect,
			LeaseID: kvapi.LeaseID(lease),
			TTLms:   ttl,
		}
		got, err := decodeCommand(encodeCommand(c))
		if err != nil {
			return false
		}
		return got.Op == c.Op && got.Key == c.Key && bytes.Equal(got.Value, c.Value) &&
			got.Expect == c.Expect && got.LeaseID == c.LeaseID && got.TTLms == c.TTLms
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 2000}); err != nil {
		t.Fatal(err)
	}
}

// TestProp_DecodeCommandNeverPanics proves the decoder rejects arbitrary bytes
// without panicking (defends the FSM apply path against malformed input).
func TestProp_DecodeCommandNeverPanics(t *testing.T) {
	f := func(data []byte) bool {
		_, _ = decodeCommand(data) // must not panic
		return true
	}
	if err := quick.Check(f, &quick.Config{MaxCount: 5000}); err != nil {
		t.Fatal(err)
	}
}

// TestProp_CRDTConverges proves that, regardless of the order random operations
// are applied and gossiped across replicas, full-state anti-entropy drives every
// replica to identical state — the core CRDT correctness property.
func TestProp_CRDTConverges(t *testing.T) {
	const replicas = 3
	keys := []string{"a", "b", "c", "d", "e"}
	rng := rand.New(rand.NewSource(1))

	for trial := 0; trial < 40; trial++ {
		engs := make([]*CRDTEngine, replicas)
		for i := range engs {
			e := NewCRDTEngine(fmt.Sprintf("n%d", i), eventbus.NewBus(), zap.NewNop())
			if err := e.Start(context.Background()); err != nil {
				t.Fatal(err)
			}
			engs[i] = e
		}

		// Random ops on random replicas.
		for op := 0; op < 60; op++ {
			e := engs[rng.Intn(replicas)]
			k := keys[rng.Intn(len(keys))]
			if rng.Intn(3) == 0 {
				_ = e.Delete(k)
			} else {
				_, _ = e.Set(k, []byte(fmt.Sprintf("v%d", rng.Intn(1000))))
			}
			// Occasionally gossip a delta both ways to a random peer.
			if rng.Intn(2) == 0 {
				p := engs[rng.Intn(replicas)]
				for _, fr := range e.DrainBroadcasts(0, 1<<20) {
					p.OnFrame(fr)
				}
			}
		}

		// Anti-entropy to quiescence: every replica merges every other's full
		// state, repeated until stable.
		for round := 0; round < replicas+2; round++ {
			for a := range engs {
				for b := range engs {
					if a != b {
						engs[b].OnFrame(engs[a].FullState())
					}
				}
			}
		}

		// All replicas must agree on every key.
		for _, k := range keys {
			want, wantErr := engs[0].Get(k)
			for i := 1; i < replicas; i++ {
				got, gotErr := engs[i].Get(k)
				if (wantErr == nil) != (gotErr == nil) {
					t.Fatalf("trial %d key %q: presence diverged n0=%v n%d=%v", trial, k, wantErr, i, gotErr)
				}
				if wantErr == nil && string(got.Value) != string(want.Value) {
					t.Fatalf("trial %d key %q: value diverged n0=%q n%d=%q", trial, k, want.Value, i, got.Value)
				}
			}
		}

		for _, e := range engs {
			_ = e.Stop()
		}
	}
}
