// SPDX-License-Identifier: MPL-2.0

package eventual

import (
	"bytes"
	"fmt"
	"testing"
)

func TestEncodeShardResponseFramesBoundedSplitsPayloads(t *testing.T) {
	sender := "node-a"
	headerLen := 2 + len(sender)
	maxBytes := headerLen + 20
	payloads := [][]byte{
		bytes.Repeat([]byte{1}, 12),
		bytes.Repeat([]byte{2}, 12),
		bytes.Repeat([]byte{3}, 4),
	}

	frames, sent, skipped, err := EncodeShardResponseFramesBounded(sender, payloads, maxBytes)
	if err != nil {
		t.Fatalf("encode bounded: %v", err)
	}
	if sent != len(payloads) || skipped != 0 {
		t.Fatalf("sent/skipped=%d/%d, want %d/0", sent, skipped, len(payloads))
	}
	if len(frames) != 2 {
		t.Fatalf("frames=%d, want 2", len(frames))
	}
	for _, frame := range frames {
		if len(frame) > maxBytes {
			t.Fatalf("frame too large: %d > %d", len(frame), maxBytes)
		}
		gotSender, _, err := DecodeShardResponseFrame(frame[1:])
		if err != nil {
			t.Fatalf("decode response frame: %v", err)
		}
		if gotSender != sender {
			t.Fatalf("sender=%q, want %q", gotSender, sender)
		}
	}
}

func TestEncodeShardResponseFramesBoundedSkipsImpossiblePayload(t *testing.T) {
	sender := "node-a"
	headerLen := 2 + len(sender)
	maxBytes := headerLen + 8
	payloads := [][]byte{
		bytes.Repeat([]byte{1}, 4),
		bytes.Repeat([]byte{2}, 9),
	}

	frames, sent, skipped, err := EncodeShardResponseFramesBounded(sender, payloads, maxBytes)
	if err != nil {
		t.Fatalf("encode bounded: %v", err)
	}
	if sent != 1 || skipped != 1 {
		t.Fatalf("sent/skipped=%d/%d, want 1/1", sent, skipped)
	}
	if len(frames) != 1 {
		t.Fatalf("frames=%d, want 1", len(frames))
	}
	if len(frames[0]) > maxBytes {
		t.Fatalf("frame too large: %d > %d", len(frames[0]), maxBytes)
	}
}

func TestEncodeShardPayloadsBoundedSplitsEntries(t *testing.T) {
	s := NewState("node-a")
	entries := make([]*Entry, 0, 3)
	for _, name := range []string{"a", "b", "c"} {
		e, _ := reg(s, name, makePID("node-a", "h", name), 100)
		entries = append(entries, e)
	}
	first, err := EncodeDelta(nil, entries[0], s.NodeString(entries[0].Node))
	if err != nil {
		t.Fatalf("encode first delta: %v", err)
	}
	maxBytes := 6 + len(first) + 1

	payloads, skipped, err := EncodeShardPayloadsBounded(3, entries, s.NodeString, maxBytes)
	if err != nil {
		t.Fatalf("encode bounded shard payloads: %v", err)
	}
	if skipped != 0 {
		t.Fatalf("skipped=%d, want 0", skipped)
	}
	if len(payloads) != len(entries) {
		t.Fatalf("payloads=%d, want %d", len(payloads), len(entries))
	}
	decoded := 0
	for _, payload := range payloads {
		if len(payload) > maxBytes {
			t.Fatalf("payload too large: %d > %d", len(payload), maxBytes)
		}
		sp, _, err := DecodeShardPayload(payload)
		if err != nil {
			t.Fatalf("decode shard payload: %v", err)
		}
		decoded += len(sp.Entries)
	}
	if decoded != len(entries) {
		t.Fatalf("decoded=%d, want %d", decoded, len(entries))
	}
}

func TestEncodeShardPayloadsBoundedSkipsImpossibleEntry(t *testing.T) {
	s := NewState("node-a")
	e, _ := reg(s, "too-large-for-this-test-budget", makePID("node-a", "h", "p"), 100)

	payloads, skipped, err := EncodeShardPayloadsBounded(3, []*Entry{e}, s.NodeString, 16)
	if err != nil {
		t.Fatalf("encode bounded shard payloads: %v", err)
	}
	if skipped != 1 {
		t.Fatalf("skipped=%d, want 1", skipped)
	}
	if len(payloads) != 0 {
		t.Fatalf("payloads=%d, want 0", len(payloads))
	}
}

func TestEncodeShardPayloadsBoundedPerformanceEnvelope(t *testing.T) {
	s := NewState("node-a")
	const n = 1000
	entries := make([]*Entry, 0, n)
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("perf-envelope-%04d", i)
		e, _ := reg(s, name, makePID("node-a", "h", fmt.Sprintf("pid-%04d", i)), 100)
		entries = append(entries, e)
	}

	maxBytes := ReliableFrameMaxBytes - 16
	payloads, skipped, err := EncodeShardPayloadsBounded(7, entries, s.NodeString, maxBytes)
	if err != nil {
		t.Fatalf("encode bounded shard payloads: %v", err)
	}
	if skipped != 0 {
		t.Fatalf("skipped=%d, want 0", skipped)
	}
	if len(payloads) == 0 || len(payloads) > 4 {
		t.Fatalf("payload count=%d, want 1..4 for %d entries", len(payloads), n)
	}
	for _, payload := range payloads {
		if len(payload) > maxBytes {
			t.Fatalf("payload too large: %d > %d", len(payload), maxBytes)
		}
	}

	allocs := testing.AllocsPerRun(10, func() {
		payloads, skipped, err := EncodeShardPayloadsBounded(7, entries, s.NodeString, maxBytes)
		if err != nil || skipped != 0 || len(payloads) == 0 {
			t.Fatalf("unexpected encode result: payloads=%d skipped=%d err=%v", len(payloads), skipped, err)
		}
	})
	if allocs > float64(n*2+100) {
		t.Fatalf("allocations too high: got %.0f for %d entries", allocs, n)
	}
}
