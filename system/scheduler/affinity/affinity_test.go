// SPDX-License-Identifier: MPL-2.0

package affinity

import (
	"context"
	"sort"
	"testing"
)

func TestPartitionContextRoundTrip(t *testing.T) {
	if _, ok := PartitionFromContext(context.Background()); ok {
		t.Fatal("expected no partition in a bare context")
	}

	p := Compute(8, 2, nil, nil)
	ctx := WithPartition(context.Background(), p)
	got, ok := PartitionFromContext(ctx)
	if !ok {
		t.Fatal("expected partition present")
	}
	if !got.Enabled || len(got.WASMCPUs) != 2 || len(got.ActorCPUs) != 6 {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
}

func TestComputeReservedSplitIsDisjointAndComplete(t *testing.T) {
	p := Compute(8, 2, nil, nil)
	if !p.Enabled {
		t.Fatal("expected enabled partition")
	}
	if got := len(p.WASMCPUs); got != 2 {
		t.Fatalf("expected 2 WASM cores, got %d", got)
	}
	if got := len(p.ActorCPUs); got != 6 {
		t.Fatalf("expected 6 actor cores, got %d", got)
	}

	seen := make(map[int]int)
	for _, c := range append(append(Set{}, p.ActorCPUs...), p.WASMCPUs...) {
		seen[c]++
	}
	if len(seen) != 8 {
		t.Fatalf("expected 8 distinct cores covering [0,8), got %d", len(seen))
	}
	for c := 0; c < 8; c++ {
		if seen[c] != 1 {
			t.Fatalf("core %d covered %d times (want exactly 1)", c, seen[c])
		}
	}
}

func TestComputeMinimalSplitIsEnabled(t *testing.T) {
	p := Compute(2, 1, nil, nil)
	if !p.Enabled {
		t.Fatalf("Compute(2,1) should be enabled, got %+v", p)
	}
	if len(p.ActorCPUs) != 1 || p.ActorCPUs[0] != 0 {
		t.Fatalf("ActorCPUs = %v, want [0]", p.ActorCPUs)
	}
	if len(p.WASMCPUs) != 1 || p.WASMCPUs[0] != 1 {
		t.Fatalf("WASMCPUs = %v, want [1]", p.WASMCPUs)
	}
}

func TestComputeExplicitListsOverride(t *testing.T) {
	p := Compute(8, 2, []int{6, 7}, []int{0, 1, 2})
	if !p.Enabled {
		t.Fatal("expected enabled partition")
	}
	want := []int{6, 7}
	got := append(Set{}, p.WASMCPUs...)
	sort.Ints(got)
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("WASMCPUs = %v, want %v", got, want)
	}
	if len(p.ActorCPUs) != 3 {
		t.Fatalf("ActorCPUs = %v, want 3 entries", p.ActorCPUs)
	}
}

func TestComputeInvalidReturnsDisabled(t *testing.T) {
	cases := []struct {
		name        string
		wasm, actor []int
		numCPU      int
		reserved    int
	}{
		{name: "reserved zero", numCPU: 8, reserved: 0},
		{name: "reserved all", numCPU: 8, reserved: 8},
		{name: "reserved over", numCPU: 8, reserved: 9},
		{name: "single cpu", numCPU: 1, reserved: 1},
		{name: "explicit one side only", numCPU: 8, reserved: 0, wasm: []int{6, 7}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := Compute(tc.numCPU, tc.reserved, tc.wasm, tc.actor)
			if p.Enabled {
				t.Fatalf("expected disabled partition, got %+v", p)
			}
			if !p.ActorCPUs.Empty() || !p.WASMCPUs.Empty() {
				t.Fatalf("expected empty sets, got %+v", p)
			}
		})
	}
}

func TestSetEmpty(t *testing.T) {
	if !(Set{}).Empty() {
		t.Fatal("empty set should report Empty() == true")
	}
	if !(Set(nil)).Empty() {
		t.Fatal("nil set should report Empty() == true")
	}
	if (Set{0}).Empty() {
		t.Fatal("non-empty set should report Empty() == false")
	}
}

func TestApplyEmptyIsNoop(t *testing.T) {
	if err := Apply(nil); err != nil {
		t.Fatalf("Apply(nil): %v", err)
	}
	if err := Apply(Set{}); err != nil {
		t.Fatalf("Apply(empty): %v", err)
	}
}
