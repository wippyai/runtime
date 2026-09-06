// SPDX-License-Identifier: MPL-2.0

package internode

// Class is the QoS class of a queued internode message AND the wire-level
// sub-protocol tag carried in the frame header. Each managed peer has one
// FIFO queue per class. Delivery policy is class-specific:
//
//   - ClassRaftControl: reliable while the peer remains managed.
//   - ClassGossip: drop-newest (memberlist/SWIM — gossip is lossy by
//     design; the next round will correct it).
//   - ClassPGBroadcast: reliable while the peer remains managed.
//   - ClassRaftRPC: raft RPC request/reply frames over internode. The name
//     is kept for wire compatibility with the prior raft class byte; it no
//     longer carries a byte stream.
//   - ClassSurface: bounded admission; accepted frames survive write retries.
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
// MUST be updated; the per-state ring slice is sized from it.
const numClasses = 5

// MetadataSurfaceProtocol advertises support before a peer emits the new
// surface class on an existing connection to a potentially older node.
const MetadataSurfaceProtocol = "tty_surface_protocol"

// MaxSurfaceFrameSize bounds queued and in-flight surface payloads to 32 MiB
// per managed peer (32 admission slots plus a reserved retry batch of 32),
// independent of the larger Raft snapshot frame limit.
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
