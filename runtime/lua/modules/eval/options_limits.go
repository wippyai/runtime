// SPDX-License-Identifier: MPL-2.0

package eval

import (
	"fmt"
	"math"

	lua "github.com/wippyai/go-lua"
	apihost "github.com/wippyai/runtime/api/host"
)

type limitKind uint8

const (
	limitMemory limitKind = iota
	limitHeapReserve
	limitTickBudget
	limitMaxSteps
	limitMailboxCapacity
	limitMaxChildren
	limitKindCount
)

type limitSpec struct {
	name   string
	min    uint64
	kind   limitKind
	uint32 bool
}

// limitSpecs lists every accepted limit key. memory_bytes aliases
// memory_limit_bytes.
var limitSpecs = [...]limitSpec{
	{name: "mailbox_capacity", kind: limitMailboxCapacity, min: 1, uint32: true},
	{name: "memory_limit_bytes", kind: limitMemory},
	{name: "memory_bytes", kind: limitMemory},
	{name: "heap_reserve_bytes", kind: limitHeapReserve},
	{name: "tick_budget", kind: limitTickBudget, min: 1, uint32: true},
	{name: "max_steps", kind: limitMaxSteps, min: 1},
	{name: "max_children", kind: limitMaxChildren, uint32: true},
}

type limitSlot struct {
	name  string
	value uint64
	seen  bool
}

type limitSet struct {
	slots [limitKindCount]limitSlot
}

// parseLimitFields reads the limit keys of tbl into limits. Names of nested
// limits are prefixed with "limits.".
func parseLimitFields(tbl *lua.LTable, nested bool, limits *limitSet) error {
	for _, spec := range limitSpecs {
		raw := tbl.RawGetString(spec.name)
		if raw == lua.LNil {
			continue
		}
		name := spec.name
		if nested {
			name = evalOptionLimits + "." + name
		}
		n, err := limitValue(raw, spec, name)
		if err != nil {
			return err
		}
		slot := &limits.slots[spec.kind]
		if slot.seen {
			return fmt.Errorf("eval %s conflicts with %s", name, slot.name)
		}
		*slot = limitSlot{name: name, value: n, seen: true}
	}
	return nil
}

func limitValue(raw lua.LValue, spec limitSpec, name string) (uint64, error) {
	n, ok := raw.(lua.LInteger)
	if !ok || n < 0 {
		return 0, fmt.Errorf("eval %s must be non-negative integer", name)
	}
	out := uint64(n)
	if out < spec.min {
		return 0, fmt.Errorf("eval %s must be greater than zero", name)
	}
	if spec.uint32 && out > math.MaxUint32 {
		return 0, fmt.Errorf("eval %s exceeds uint32", name)
	}
	return out, nil
}

func (l *limitSet) apply(policy *apihost.EvalPolicy) {
	if s := l.slots[limitMemory]; s.seen {
		policy.MemoryLimitBytes = s.value
	}
	if s := l.slots[limitHeapReserve]; s.seen {
		policy.HeapReserveBytes = s.value
	}
	if s := l.slots[limitTickBudget]; s.seen {
		policy.TickBudget = uint32(s.value)
	}
	if s := l.slots[limitMaxSteps]; s.seen {
		policy.MaxSteps = s.value
	}
	if s := l.slots[limitMailboxCapacity]; s.seen {
		policy.MailboxCapacity = uint32(s.value)
	}
	if s := l.slots[limitMaxChildren]; s.seen {
		policy.MaxChildren = uint32(s.value)
	}
}
