// SPDX-License-Identifier: MPL-2.0

package process

// Preemptible is an optional interface for processes that can suspend a
// running step at a safepoint once they have used up their execution slice.
// A scheduler calls EnablePreemption once, when it admits the process, and
// the opt-in persists for the process's lifetime. It then runs the process
// again after Step reports StepPreempted; a process that was never enabled
// runs each step to a blocking point or completion.
type Preemptible interface {
	EnablePreemption()
}

// StepAccounted is an optional interface for processes that count their
// scheduler steps against a limit. A scheduler carries the count from a
// process to its upgrade replacement so the limit covers the whole lifetime
// of the actor rather than each incarnation.
type StepAccounted interface {
	// StepsUsed returns the number of steps taken so far.
	StepsUsed() uint64
	// ResumeStepCount sets the number of steps already taken. It is called
	// after Init, before the first Step.
	ResumeStepCount(used uint64)
}
