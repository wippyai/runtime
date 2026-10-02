// SPDX-License-Identifier: MPL-2.0

package lua

import (
	"encoding/json"
	"testing"

	"github.com/wippyai/runtime/api/attrs"
)

func TestEntryExecutionBudgetsFromMetaOptions(t *testing.T) {
	b, err := EntryExecutionBudgets(attrs.Bag{"options": map[string]any{"tick_budget": 2048, "max_steps": float64(10)}})
	if err != nil {
		t.Fatal(err)
	}
	if !b.TickBudgetSet || b.TickBudget != 2048 || !b.MaxStepsSet || b.MaxSteps != 10 {
		t.Fatalf("unexpected budgets %+v", b)
	}

	none, err := EntryExecutionBudgets(attrs.Bag{"comment": "x"})
	if err != nil || none.TickBudgetSet || none.MaxStepsSet {
		t.Fatalf("expected no budgets, got %+v %v", none, err)
	}
}

func TestEntryExecutionBudgetsRejectsDirectMetaKeys(t *testing.T) {
	for _, key := range []string{ProcessOptionTickBudget, ProcessOptionMaxSteps} {
		if _, err := EntryExecutionBudgets(attrs.Bag{key: 10}); err == nil {
			t.Fatalf("%s directly under meta must be rejected", key)
		}
	}
}

func TestExecutionBudgetsFromOptionsValues(t *testing.T) {
	valid := []struct {
		raw  any
		want int64
	}{
		{int(-1), -1},
		{int64(512), 512},
		{uint32(7), 7},
		{float64(4096), 4096},
		{json.Number("100"), 100},
	}
	for _, tc := range valid {
		b, err := ExecutionBudgetsFromOptions(attrs.Bag{ProcessOptionTickBudget: tc.raw}, "test")
		if err != nil || b.TickBudget != tc.want || !b.TickBudgetSet {
			t.Fatalf("tick_budget %v: got %+v %v", tc.raw, b, err)
		}
	}
	invalid := []attrs.Bag{
		{ProcessOptionTickBudget: "512"},
		{ProcessOptionTickBudget: 1.5},
		{ProcessOptionTickBudget: float64(1 << 60)},
		{ProcessOptionMaxSteps: -1},
		{ProcessOptionMaxSteps: true},
	}
	for _, opts := range invalid {
		if _, err := ExecutionBudgetsFromOptions(opts, "test"); err == nil {
			t.Fatalf("expected %v to be rejected", opts)
		}
	}
}

func TestExecutionBudgetsOverride(t *testing.T) {
	entry := ExecutionBudgets{TickBudget: 100, TickBudgetSet: true, MaxSteps: 5, MaxStepsSet: true}
	got := entry.Override(ExecutionBudgets{TickBudget: -1, TickBudgetSet: true})
	if got.TickBudget != -1 || got.MaxSteps != 5 || !got.MaxStepsSet {
		t.Fatalf("unexpected override %+v", got)
	}
}
