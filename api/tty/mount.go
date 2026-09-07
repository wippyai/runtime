// SPDX-License-Identifier: MPL-2.0

package tty

import (
	"context"

	"github.com/wippyai/runtime/api/pid"
)

// MountRights are independent capabilities. No rights are implied by an
// address, by mesh membership, or by another right.
type MountRights struct {
	Observe bool
	Input   bool
	Resize  bool
}

const (
	RightObserve = "tty.observe"
	RightInput   = "tty.input"
	RightResize  = "tty.resize"
)

// MountableViewport delegates access to one exact process. Only the original
// viewport owner can issue or revoke mounts; mounted views cannot re-delegate.
type MountableViewport interface {
	Mount(context.Context, pid.PID, MountRights) (string, error)
	Revoke(context.Context, string) error
}

// CheckedViewport validates process ownership and rights before Lua accesses
// a view, including snapshots already cached on the local node.
type CheckedViewport interface {
	Check(context.Context, string) error
}

// RemoteViewport exposes cancellable operations to the yielding dispatcher.
// Snapshot and Updates always read the local cache and never wait for a peer.
type RemoteViewport interface {
	Viewport
	SendContext(context.Context, Event) error
	ResizeContext(context.Context, int, int) error
}

type RemoteService interface {
	IsRemote(string) bool
}

// MeshTransport is a dedicated, authenticated peer protocol. Send must enqueue
// without waiting for network I/O and must return an error at its queue limit.
// Receive callbacks must never be invoked with a peer identity from payloads.
type MeshTransport interface {
	Send(peer string, data []byte) error
	Receive(func(peer string, data []byte)) error
}

// MeshPeerChecker optionally rejects peers lacking surface protocol support
// before sending a new class byte on a shared mesh connection.
type MeshPeerChecker interface{ CheckPeer(string) error }
