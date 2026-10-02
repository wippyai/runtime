// SPDX-License-Identifier: MPL-2.0

package process

// Preemptible is an optional interface for processes that can suspend a
// running step at a safepoint once they have used up their execution slice.
// A scheduler calls EnablePreemption when it runs the process again after
// Step reports StepPreempted; a process that was never enabled runs each step
// to a blocking point or completion.
type Preemptible interface {
	EnablePreemption()
}
