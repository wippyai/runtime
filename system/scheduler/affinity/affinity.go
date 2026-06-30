// SPDX-License-Identifier: MPL-2.0

// Package affinity partitions logical CPUs into disjoint sets and pins the
// current OS thread to a set. It lets the runtime keep CPU-bound WASM execution
// on a reserved group of cores, away from the cores the actor scheduler uses.
//
// Pinning is only effective on Linux; on every other platform Apply is a no-op
// so callers can wire it unconditionally and rely on build-tagged behavior.
package affinity

// Set is a list of logical CPU indices.
type Set []int

// Empty reports whether the set selects no CPUs.
func (s Set) Empty() bool { return len(s) == 0 }

// Partition holds the disjoint CPU sets for the actor scheduler and the WASM
// pool. When Enabled is false both sets are empty and every Apply is a no-op.
type Partition struct {
	ActorCPUs Set
	WASMCPUs  Set
	Enabled   bool
}

// Apply pins the current OS thread to the given CPU set. The caller MUST have
// already locked its goroutine to the thread via runtime.LockOSThread. A nil or
// empty set is a no-op. On platforms without affinity support Apply is a no-op
// and returns nil.
func Apply(set Set) error {
	if len(set) == 0 {
		return nil
	}
	return pin(set)
}

// Compute partitions numCPU logical cores into disjoint actor and WASM sets.
//
// Explicit wasmCPUs/actorCPUs override reservedWASM and are used verbatim.
// Otherwise the highest-indexed reservedWASM cores are reserved for WASM and the
// remainder go to the actor scheduler. The split is rejected (returning a
// disabled, empty partition) when it would leave either side without a core.
func Compute(numCPU, reservedWASM int, wasmCPUs, actorCPUs []int) Partition {
	if len(wasmCPUs) > 0 || len(actorCPUs) > 0 {
		if len(wasmCPUs) == 0 || len(actorCPUs) == 0 {
			return Partition{}
		}
		return Partition{Enabled: true, ActorCPUs: Set(actorCPUs), WASMCPUs: Set(wasmCPUs)}
	}

	if numCPU < 2 || reservedWASM < 1 || reservedWASM >= numCPU {
		return Partition{}
	}

	actor := make(Set, 0, numCPU-reservedWASM)
	wasm := make(Set, 0, reservedWASM)
	for cpu := 0; cpu < numCPU; cpu++ {
		if cpu >= numCPU-reservedWASM {
			wasm = append(wasm, cpu)
		} else {
			actor = append(actor, cpu)
		}
	}
	return Partition{Enabled: true, ActorCPUs: actor, WASMCPUs: wasm}
}
