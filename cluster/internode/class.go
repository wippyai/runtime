// SPDX-License-Identifier: MPL-2.0

package internode

// Class is the QoS class of a queued internode message AND the wire-level
// sub-protocol tag carried in the frame header. Each managed peer has one
// FIFO queue per class. Every class except gossip is sequenced on the peer
// session: an accepted frame is delivered exactly once and in order while
// the session lives. Admission policy is class-specific:
//
//   - ClassRaftControl: unbounded while the peer remains managed.
//   - ClassGossip: drop-newest and unsequenced (memberlist/SWIM — gossip is
//     lossy by design; the next round will correct it).
//   - ClassPGBroadcast: unbounded while the peer remains managed.
//   - ClassRaftRPC: raft RPC request/reply frames over internode.
//   - ClassSurface: bounded admission of surfaceQueueCap frames.
type Class uint8

const (
	ClassRaftControl Class = iota
	ClassGossip
	ClassPGBroadcast
	ClassRaftRPC
	// ClassSurface carries bounded terminal mount traffic. Both peers must
	// support this protocol before a surface mount is used.
	ClassSurface
)

// numClasses is the count of Class values. If a new Class is added, this
// MUST be updated; the per-state queue array is sized from it.
const numClasses = 5

// Session control frames share the class byte of the frame header but are
// never queued or dispatched to handlers.
const (
	// classResume opens every connection of a session: seq carries the
	// sender's session ID, ack its receive cursor, and the 8-byte payload the
	// sender's view of the receiver's session ID.
	classResume Class = 0xFE
	// classAck carries only the cumulative ack.
	classAck Class = 0xFF
)

// surfaceQueueCap bounds queued surface frames per managed peer.
const surfaceQueueCap = 32

// sequenced reports whether frames of class are sequenced on the session.
func (c Class) sequenced() bool { return c != ClassGossip }

// MetadataSurfaceProtocol advertises support before a peer emits the new
// surface class on an existing connection to a potentially older node.
const MetadataSurfaceProtocol = "tty_surface_protocol"

// MaxSurfaceFrameSize bounds one surface payload, independent of the larger
// Raft snapshot frame limit. Queued surface payloads per managed peer are
// bounded by surfaceQueueCap slots; in-flight ones share the link window.
const MaxSurfaceFrameSize = 512 << 10

// String renders Class for log/metric labels.
func (c Class) String() string {
	switch c {
	case ClassRaftControl:
		return "raft"
	case ClassGossip:
		return "gossip"
	case ClassPGBroadcast:
		return "pg"
	case ClassRaftRPC:
		return "raft-rpc"
	case ClassSurface:
		return "surface"
	default:
		return "unknown"
	}
}

// ClassForTopic maps a relay package topic to its QoS class. Membership
// and discovery topics are control-plane; everything else is treated as
// application broadcast. Both are reliable while the peer remains managed.
//
// Importing `runtime/api/pg` would create a cycle (internode → pg → internode),
// so the topic strings are duplicated here as constants. They MUST stay in
// sync with `runtime/api/pg/pg.go`.
func ClassForTopic(topic string) Class {
	switch topic {
	case "pg.join", "pg.leave", "pg.discover", "pg.sync":
		return ClassRaftControl
	default:
		return ClassPGBroadcast
	}
}

// MetadataSurfaceGraphics enables optional image metadata and resource ops
// within the existing surface class. Surface protocol remains version 1.
const MetadataSurfaceGraphics = "tty_surface_graphics"
