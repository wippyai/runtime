// SPDX-License-Identifier: MPL-2.0

package lua

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"testing"

	"github.com/wippyai/runtime/api/attrs"
)

// optionKinds is the number of Go representations fuzzOptionValue produces.
const optionKinds = 21

// fuzzOptionValue builds an option value of the representation selected by
// kind from the fuzzed scalars.
func fuzzOptionValue(kind uint8, i int64, u uint64, f float64, s string) any {
	switch kind % optionKinds {
	case 0:
		return int(i)
	case 1:
		return int8(i)
	case 2:
		return int16(i)
	case 3:
		return int32(i)
	case 4:
		return i
	case 5:
		return uint(u)
	case 6:
		return uint8(u)
	case 7:
		return uint16(u)
	case 8:
		return uint32(u)
	case 9:
		return u
	case 10:
		return float32(f)
	case 11:
		return f
	case 12:
		return json.Number(s)
	case 13:
		return s
	case 14:
		return i%2 == 0
	case 15:
		return nil
	case 16:
		return []any{i}
	case 17:
		return map[string]any{"v": i}
	case 18:
		return []byte(s)
	case 19:
		return uintptr(u)
	default:
		return struct{}{}
	}
}

// wantTick is the reference reading of a tick_budget value: whether it is
// given, whether it is a valid integer, and its value.
func wantTick(raw any) (given, valid bool, v int64) {
	switch x := raw.(type) {
	case nil:
		return false, false, 0
	case int:
		return true, true, int64(x)
	case int8:
		return true, true, int64(x)
	case int16:
		return true, true, int64(x)
	case int32:
		return true, true, int64(x)
	case int64:
		return true, true, x
	case uint:
		return true, uint64(x) <= math.MaxInt64, int64(x)
	case uint8:
		return true, true, int64(x)
	case uint16:
		return true, true, int64(x)
	case uint32:
		return true, true, int64(x)
	case uint64:
		return true, x <= math.MaxInt64, int64(x)
	case uintptr:
		return true, uint64(x) <= math.MaxInt64, int64(x)
	case float32:
		return wantFloat(float64(x), true)
	case float64:
		return wantFloat(x, true)
	case json.Number:
		n, err := strconv.ParseInt(string(x), 10, 64)
		return true, err == nil, n
	}
	return true, false, 0
}

// wantSteps is the reference reading of a max_steps value, which spans the
// whole uint64 range and rejects negatives.
func wantSteps(raw any) (given, valid bool, v uint64) {
	switch x := raw.(type) {
	case nil:
		return false, false, 0
	case int:
		return true, x >= 0, uint64(x)
	case int8:
		return true, x >= 0, uint64(x)
	case int16:
		return true, x >= 0, uint64(x)
	case int32:
		return true, x >= 0, uint64(x)
	case int64:
		return true, x >= 0, uint64(x)
	case uint:
		return true, true, uint64(x)
	case uint8:
		return true, true, uint64(x)
	case uint16:
		return true, true, uint64(x)
	case uint32:
		return true, true, uint64(x)
	case uint64:
		return true, true, x
	case uintptr:
		return true, true, uint64(x)
	case float32:
		g, ok, n := wantFloat(float64(x), false)
		return g, ok, uint64(n)
	case float64:
		g, ok, n := wantFloat(x, false)
		return g, ok, uint64(n)
	case json.Number:
		n, err := strconv.ParseUint(string(x), 10, 64)
		return true, err == nil, n
	}
	return true, false, 0
}

func wantFloat(f float64, signed bool) (given, valid bool, v int64) {
	if math.IsNaN(f) || math.IsInf(f, 0) || f != math.Trunc(f) || f > 1<<53 || f < -(1<<53) || (!signed && f < 0) {
		return true, false, 0
	}
	return true, true, int64(f)
}

func checkTickBudget(t *testing.T, b ExecutionBudgets, err error, raw any) {
	t.Helper()
	given, valid, want := wantTick(raw)
	switch {
	case !given:
		if err != nil || b.TickBudgetSet || b.TickBudget != 0 {
			t.Fatalf("tick_budget %#v: absent option parsed to %+v, %v", raw, b, err)
		}
	case !valid:
		if err == nil {
			t.Fatalf("tick_budget %#v: expected an error, got %+v", raw, b)
		}
	default:
		if err != nil || !b.TickBudgetSet || b.TickBudget != want {
			t.Fatalf("tick_budget %#v: got %+v, %v, want %d", raw, b, err, want)
		}
	}
}

func checkMaxSteps(t *testing.T, b ExecutionBudgets, err error, raw any) {
	t.Helper()
	given, valid, want := wantSteps(raw)
	switch {
	case !given:
		if err != nil || b.MaxStepsSet || b.MaxSteps != 0 {
			t.Fatalf("max_steps %#v: absent option parsed to %+v, %v", raw, b, err)
		}
	case !valid:
		if err == nil {
			t.Fatalf("max_steps %#v: expected an error, got %+v", raw, b)
		}
	default:
		if err != nil || !b.MaxStepsSet || b.MaxSteps != want {
			t.Fatalf("max_steps %#v: got %+v, %v, want %d", raw, b, err, want)
		}
	}
}

func addOptionSeeds(f *testing.F) {
	f.Helper()
	ints := []int64{0, 1, -1, 2048, math.MaxInt64, math.MinInt64, 1 << 53, 1<<53 + 1}
	f.Add(uint8(9), int64(0), uint64(math.MaxUint64), 0.0, "", uint8(9), int64(0), uint64(math.MaxUint64), 0.0, "18446744073709551615")
	for kind := uint8(0); kind < optionKinds; kind++ {
		for _, i := range ints {
			f.Add(kind, i, uint64(i), float64(i), "100", kind+1, i, uint64(i), float64(i), "7")
		}
		f.Add(kind, int64(5), uint64(math.MaxUint64), math.NaN(), "1.5", kind, int64(-5), uint64(1)<<63, math.Inf(1), "1e3")
		f.Add(kind, int64(5), uint64(5), 0.5, "", kind, int64(5), uint64(5), -0.0, "99999999999999999999")
	}
}

// ExecutionBudgetsFromOptions reads tick_budget and max_steps with the
// documented semantics for every value representation.
func FuzzExecutionBudgetsFromOptions(f *testing.F) {
	addOptionSeeds(f)
	f.Fuzz(func(t *testing.T, tk uint8, ti int64, tu uint64, tf float64, ts string, mk uint8, mi int64, mu uint64, mf float64, ms string) {
		tick := fuzzOptionValue(tk, ti, tu, tf, ts)
		steps := fuzzOptionValue(mk, mi, mu, mf, ms)

		b, err := ExecutionBudgetsFromOptions(attrs.Bag{ProcessOptionTickBudget: tick}, "fuzz")
		checkTickBudget(t, b, err, tick)
		if b.MaxStepsSet {
			t.Fatalf("max_steps reported set by tick_budget only: %+v", b)
		}

		b, err = ExecutionBudgetsFromOptions(attrs.Bag{ProcessOptionMaxSteps: steps}, "fuzz")
		checkMaxSteps(t, b, err, steps)
		if b.TickBudgetSet {
			t.Fatalf("tick_budget reported set by max_steps only: %+v", b)
		}

		both, err := ExecutionBudgetsFromOptions(attrs.Bag{ProcessOptionTickBudget: tick, ProcessOptionMaxSteps: steps}, "fuzz")
		tg, tv, _ := wantTick(tick)
		sg, sv, _ := wantSteps(steps)
		wantErr := (tg && !tv) || (sg && !sv)
		if wantErr != (err != nil) {
			t.Fatalf("both %#v %#v: error %v, want error %v", tick, steps, err, wantErr)
		}
		if err == nil {
			single, _ := ExecutionBudgetsFromOptions(attrs.Bag{ProcessOptionTickBudget: tick}, "fuzz")
			other, _ := ExecutionBudgetsFromOptions(attrs.Bag{ProcessOptionMaxSteps: steps}, "fuzz")
			if both.TickBudget != single.TickBudget || both.TickBudgetSet != single.TickBudgetSet ||
				both.MaxSteps != other.MaxSteps || both.MaxStepsSet != other.MaxStepsSet {
				t.Fatalf("options are not independent: both %+v tick %+v steps %+v", both, single, other)
			}
		}
	})
}

// wrapOptions places the options under meta.options in one of the map
// representations an entry meta can hold.
func wrapOptions(shape uint8, options attrs.Bag) any {
	switch shape % 3 {
	case 0:
		return map[string]any(options)
	case 1:
		return options
	default:
		return attrs.Attributes(options)
	}
}

// EntryExecutionBudgets reads only meta.options and rejects the options
// placed directly in meta or in a non-map meta.options.
func FuzzEntryExecutionBudgets(f *testing.F) {
	f.Add(uint8(0), uint8(4), int64(2048), uint64(0), 0.0, "", uint8(4), int64(10), uint64(0), 0.0, "", uint8(0), uint8(0))
	f.Add(uint8(1), uint8(11), int64(0), uint64(0), 1.5, "", uint8(13), int64(0), uint64(0), 0.0, "x", uint8(1), uint8(1))
	f.Add(uint8(2), uint8(4), int64(-1), uint64(0), 0.0, "", uint8(4), int64(-1), uint64(0), 0.0, "", uint8(2), uint8(2))
	f.Fuzz(func(t *testing.T, shape uint8, tk uint8, ti int64, tu uint64, tf float64, ts string, mk uint8, mi int64, mu uint64, mf float64, ms string, direct uint8, outer uint8) {
		tick := fuzzOptionValue(tk, ti, tu, tf, ts)
		steps := fuzzOptionValue(mk, mi, mu, mf, ms)
		options := attrs.Bag{ProcessOptionTickBudget: tick, ProcessOptionMaxSteps: steps}

		meta := attrs.Bag{entryOptionsMetaKey: wrapOptions(shape, options), "comment": "x"}
		switch direct % 3 {
		case 1:
			meta[ProcessOptionTickBudget] = ti
		case 2:
			meta[ProcessOptionMaxSteps] = mi
		}
		got, err := EntryExecutionBudgets(meta)
		if direct%3 != 0 {
			if err == nil {
				t.Fatalf("option placed directly in meta accepted: %+v", got)
			}
			return
		}
		want, wantErr := ExecutionBudgetsFromOptions(options, "entry")
		if (err != nil) != (wantErr != nil) {
			t.Fatalf("meta.options %#v: error %v, direct parse error %v", options, err, wantErr)
		}
		if err == nil && got != want {
			t.Fatalf("meta.options %#v: got %+v, direct parse %+v", options, got, want)
		}

		scalars := []any{tick, steps, "fast", int64(5), true, []any{"tick_budget"}, 1.5}
		raw := scalars[int(outer)%len(scalars)]
		_, err = EntryExecutionBudgets(attrs.Bag{entryOptionsMetaKey: raw})
		switch raw.(type) {
		case map[string]any, attrs.Bag:
			if err != nil {
				t.Fatalf("map meta.options rejected: %v", err)
			}
		case nil:
			if err != nil {
				t.Fatalf("null meta.options rejected: %v", err)
			}
		default:
			if err == nil {
				t.Fatalf("meta.options %T accepted", raw)
			}
		}
	})
}

// Parsing an entry and a spawn bag then overriding follows the precedence
// rule: an option the spawn gives replaces the entry's, even an explicit zero,
// and an option it omits leaves the entry's value.
func FuzzExecutionBudgetsOverride(f *testing.F) {
	f.Add(int64(100), int64(5), uint8(0), int64(-1), int64(0), uint8(1), uint8(1))
	f.Add(int64(0), int64(0), uint8(2), int64(7), int64(7), uint8(0), uint8(2))
	f.Add(int64(-1), int64(1<<40), uint8(3), int64(0), int64(0), uint8(3), uint8(3))
	f.Fuzz(func(t *testing.T, et, es int64, em uint8, st, ss int64, sm uint8, _ uint8) {
		entryOpts := attrs.Bag{}
		spawnOpts := attrs.Bag{}
		if em&1 != 0 {
			entryOpts[ProcessOptionTickBudget] = et
		}
		if em&2 != 0 {
			entryOpts[ProcessOptionMaxSteps] = es
		}
		if sm&1 != 0 {
			spawnOpts[ProcessOptionTickBudget] = st
		}
		if sm&2 != 0 {
			spawnOpts[ProcessOptionMaxSteps] = ss
		}
		entry, entryErr := EntryExecutionBudgets(attrs.Bag{entryOptionsMetaKey: entryOpts})
		spawn, spawnErr := ExecutionBudgetsFromOptions(spawnOpts, "spawn")
		if entryErr != nil || spawnErr != nil {
			// Only max_steps below zero is invalid among int64 inputs.
			if (entryErr != nil) != (em&2 != 0 && es < 0) || (spawnErr != nil) != (sm&2 != 0 && ss < 0) {
				t.Fatalf("unexpected errors: entry %v spawn %v", entryErr, spawnErr)
			}
			return
		}
		got := entry.Override(spawn)

		wantTick, wantTickSet := entry.TickBudget, entry.TickBudgetSet
		if sm&1 != 0 {
			wantTick, wantTickSet = st, true
		}
		wantSteps, wantStepsSet := entry.MaxSteps, entry.MaxStepsSet
		if sm&2 != 0 {
			wantSteps, wantStepsSet = uint64(ss), true
		}
		if got.TickBudget != wantTick || got.TickBudgetSet != wantTickSet || got.MaxSteps != wantSteps || got.MaxStepsSet != wantStepsSet {
			t.Fatalf("override(%+v, %+v) = %+v", entry, spawn, got)
		}
		if (ExecutionBudgets{}).Override(spawn) != spawn {
			t.Fatalf("zero budgets overridden by %+v changed it", spawn)
		}
		if entry.Override(ExecutionBudgets{}) != entry {
			t.Fatalf("empty override changed %+v", entry)
		}
	})
}

// A process entry accepts the v2 process option keys and default_host in
// meta.options, and rejects every other key by name.
func FuzzEntryOptionKeys(f *testing.F) {
	for _, k := range []string{"max_step", "tick_budgets", "default_host", "network", "hot_policy_id", "mailbox_capacity", "", "TICK_BUDGET", "pool"} {
		f.Add(k, int64(3))
	}
	known := map[string]bool{
		"mailbox_capacity": true, "memory_limit_bytes": true, "heap_reserve_bytes": true,
		"hot_policy_id": true, "network": true, "default_host": true,
	}
	f.Fuzz(func(t *testing.T, key string, v int64) {
		if key == ProcessOptionTickBudget || key == ProcessOptionMaxSteps {
			t.Skip()
		}
		_, err := EntryExecutionBudgets(attrs.Bag{entryOptionsMetaKey: attrs.Bag{key: v}})
		if known[key] {
			if err != nil {
				t.Fatalf("known option %q rejected: %v", key, err)
			}
			return
		}
		want := fmt.Sprintf("process entry meta.options has unknown field %q", key)
		if err == nil || err.Error() != want {
			t.Fatalf("unknown option %q: got %v, want %q", key, err, want)
		}
	})
}
