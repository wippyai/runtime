// SPDX-License-Identifier: MPL-2.0

package lua

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"

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

// Process entry option keys other than the execution limits. They are
// accepted in meta.options so declarations written for the v2 runtime load
// unchanged; only the execution limits are read here.
const (
	ProcessOptionMailboxCapacity  = "mailbox_capacity"
	ProcessOptionMemoryLimitBytes = "memory_limit_bytes"
	ProcessOptionHeapReserveBytes = "heap_reserve_bytes"
	ProcessOptionHotPolicyID      = "hot_policy_id"
	ProcessOptionNetwork          = "network"
	// ProcessOptionDefaultHost names the host that serves the entry as a
	// function; the process function listener reads it from meta.options.
	ProcessOptionDefaultHost = "default_host"
)

// IsProcessEntryOptionKey reports whether key is accepted in the meta.options
// of a process entry.
func IsProcessEntryOptionKey(key string) bool {
	switch key {
	case ProcessOptionTickBudget, ProcessOptionMaxSteps, ProcessOptionMailboxCapacity,
		ProcessOptionMemoryLimitBytes, ProcessOptionHeapReserveBytes, ProcessOptionHotPolicyID,
		ProcessOptionNetwork, ProcessOptionDefaultHost:
		return true
	}
	return false
}

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
		v, err := optionUint64(raw)
		if err != nil {
			return b, fmt.Errorf("%s %q must be a non-negative integer", subject, ProcessOptionMaxSteps)
		}
		b.MaxSteps, b.MaxStepsSet = v, true
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
	for _, key := range sortedKeys(options) {
		if !IsProcessEntryOptionKey(key) {
			return ExecutionBudgets{}, fmt.Errorf("process entry meta.%s has unknown field %q", entryOptionsMetaKey, key)
		}
	}
	return ExecutionBudgetsFromOptions(options, "process entry meta.options")
}

func sortedKeys(options attrs.Bag) []string {
	keys := make([]string, 0, len(options))
	for key := range options {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
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
		return int64FromUint64(uint64(v))
	case uint8:
		return int64(v), nil
	case uint16:
		return int64(v), nil
	case uint32:
		return int64(v), nil
	case uint64:
		return int64FromUint64(v)
	case uintptr:
		return int64FromUint64(uint64(v))
	case float32:
		return int64FromFloat(float64(v))
	case float64:
		return int64FromFloat(v)
	case json.Number:
		return v.Int64()
	}
	return 0, fmt.Errorf("unsupported %T", raw)
}

func optionUint64(raw any) (uint64, error) {
	switch v := raw.(type) {
	case uint:
		return uint64(v), nil
	case uint8:
		return uint64(v), nil
	case uint16:
		return uint64(v), nil
	case uint32:
		return uint64(v), nil
	case uint64:
		return v, nil
	case uintptr:
		return uint64(v), nil
	case int:
		return nonNegative(uint64(v), v < 0)
	case int8:
		return nonNegative(uint64(v), v < 0)
	case int16:
		return nonNegative(uint64(v), v < 0)
	case int32:
		return nonNegative(uint64(v), v < 0)
	case int64:
		return nonNegative(uint64(v), v < 0)
	case float32:
		return uint64FromFloat(float64(v))
	case float64:
		return uint64FromFloat(v)
	case json.Number:
		return strconv.ParseUint(v.String(), 10, 64)
	}
	return 0, fmt.Errorf("unsupported %T", raw)
}

func nonNegative(v uint64, negative bool) (uint64, error) {
	if negative {
		return 0, fmt.Errorf("negative")
	}
	return v, nil
}

func int64FromUint64(v uint64) (int64, error) {
	if v > math.MaxInt64 {
		return 0, fmt.Errorf("overflow")
	}
	return int64(v), nil
}

func int64FromFloat(v float64) (int64, error) {
	if math.IsNaN(v) || math.IsInf(v, 0) || v < -maxExactFloatInteger || v > maxExactFloatInteger || math.Trunc(v) != v {
		return 0, fmt.Errorf("invalid float")
	}
	return int64(v), nil
}

func uint64FromFloat(v float64) (uint64, error) {
	if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > maxExactFloatInteger || math.Trunc(v) != v {
		return 0, fmt.Errorf("invalid float")
	}
	return uint64(v), nil
}
