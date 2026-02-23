// SPDX-License-Identifier: MPL-2.0

package registry

// DispatchMode controls whether registry operations emit events.
type DispatchMode int

const (
	// DispatchEvents sends operations through the event bus.
	DispatchEvents DispatchMode = iota
	// DispatchInternal applies operations internally without emitting events.
	DispatchInternal
)

// DispatchPolicy decides how an operation should be dispatched.
type DispatchPolicy interface {
	Mode(Operation) DispatchMode
}
