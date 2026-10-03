// SPDX-License-Identifier: MPL-2.0

package lua

import (
	"encoding/json"
	"fmt"
	"math"

	"github.com/wippyai/runtime/api/attrs"
)

// Actor execution option keys, shared by process entry meta.options and
// process spawn options.
const (
	ProcessOptionTickBudget = "tick_budget"
	ProcessOptionMaxSteps   = "max_steps"
)

// entryOptionsMetaKey is the entry meta bag holding declaration-time options.
const entryOptionsMetaKey = "options"

// ExecutionBudgets holds the execution limits of a Lua actor.
//
// TickBudget is the number of VM ticks (backward jumps, loop iterations and
// calls) an actor may run before it is preempted so other actors can run:
// zero selects the runtime default and a negative value disables preemption.
// MaxSteps bounds the number of scheduler steps the actor may take; zero is
// unlimited and exceeding it fails the actor with process.ErrStepLimitExceeded.
type ExecutionBudgets struct {
	TickBudget int64
	MaxSteps   uint64
	// TickBudgetSet and MaxStepsSet report that the option was given, so an
	// explicit zero overrides a lower-precedence value.
	TickBudgetSet bool
	MaxStepsSet   bool
}

// Override returns b with the options set in o taking precedence.
func (b ExecutionBudgets) Override(o ExecutionBudgets) ExecutionBudgets {
	if o.TickBudgetSet {
		b.TickBudget, b.TickBudgetSet = o.TickBudget, true
	}
	if o.MaxStepsSet {
		b.MaxSteps, b.MaxStepsSet = o.MaxSteps, true
	}
	return b
}

// ExecutionBudgetsFromOptions reads the execution options from an options
// bag; nil options hold no execution options. subject names the source in
// error messages.
func ExecutionBudgetsFromOptions(options attrs.Attributes, subject string) (ExecutionBudgets, error) {
	var b ExecutionBudgets
	if options == nil {
		return b, nil
	}
	if raw, ok := options.Get(ProcessOptionTickBudget); ok && raw != nil {
		v, err := optionInt64(raw)
		if err != nil {
			return b, fmt.Errorf("%s %q must be an integer", subject, ProcessOptionTickBudget)
		}
		b.TickBudget, b.TickBudgetSet = v, true
	}
	if raw, ok := options.Get(ProcessOptionMaxSteps); ok && raw != nil {
		v, err := optionInt64(raw)
		if err != nil || v < 0 {
			return b, fmt.Errorf("%s %q must be a non-negative integer", subject, ProcessOptionMaxSteps)
		}
		b.MaxSteps, b.MaxStepsSet = uint64(v), true
	}
	return b, nil
}

// EntryExecutionBudgets reads the execution options of a process entry from
// meta.options, which must be a map when present. The options are only accepted nested under meta.options.
func EntryExecutionBudgets(meta attrs.Bag) (ExecutionBudgets, error) {
	for _, key := range [...]string{ProcessOptionTickBudget, ProcessOptionMaxSteps} {
		if _, ok := meta.Get(key); ok {
			return ExecutionBudgets{}, fmt.Errorf("process entry meta %q must be nested under meta.%s", key, entryOptionsMetaKey)
		}
	}
	options, ok := meta.GetBag(entryOptionsMetaKey)
	if !ok {
		if raw, present := meta.Get(entryOptionsMetaKey); present && raw != nil {
			return ExecutionBudgets{}, fmt.Errorf("process entry meta.%s must be a map, got %T", entryOptionsMetaKey, raw)
		}
		return ExecutionBudgets{}, nil
	}
	return ExecutionBudgetsFromOptions(options, "process entry meta.options")
}

// maxExactFloatInteger is the largest integer a float64 holds exactly.
const maxExactFloatInteger = 1 << 53

func optionInt64(raw any) (int64, error) {
	switch v := raw.(type) {
	case int:
		return int64(v), nil
	case int8:
		return int64(v), nil
	case int16:
		return int64(v), nil
	case int32:
		return int64(v), nil
	case int64:
		return v, nil
	case uint:
		return uintOption(uint64(v))
	case uint8:
		return int64(v), nil
	case uint16:
		return int64(v), nil
	case uint32:
		return int64(v), nil
	case uint64:
		return uintOption(v)
	case float32:
		return floatOption(float64(v))
	case float64:
		return floatOption(v)
	case json.Number:
		return v.Int64()
	}
	return 0, fmt.Errorf("not an integer: %T", raw)
}

func uintOption(v uint64) (int64, error) {
	if v > math.MaxInt64 {
		return 0, fmt.Errorf("integer overflows int64: %d", v)
	}
	return int64(v), nil
}

func floatOption(v float64) (int64, error) {
	if v != math.Trunc(v) || math.Abs(v) > maxExactFloatInteger {
		return 0, fmt.Errorf("not an exact integer: %v", v)
	}
	return int64(v), nil
}
