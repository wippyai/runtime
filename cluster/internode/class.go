// SPDX-License-Identifier: MPL-2.0

package internode

// Class is the QoS class of a queued internode message AND the wire-level
// sub-protocol tag carried in the frame header. Each managed peer has one
// ring buffer per class. Drop policy is class-specific:
//
//   - ClassRaftControl: drop-oldest (etcd/raft semantics — newer state
//     wins; control RPCs are idempotent).
//   - ClassGossip: drop-newest (memberlist/SWIM — gossip is lossy by
//     design; the next round will correct it).
//   - ClassPGBroadcast: drop-newest with caller error (Erlang OTP `pg` —
//     fire-and-forget, but observable).
//   - ClassRaftMesh: drop-oldest. Carries multiplexed Raft transport
//     frames so hashicorp/raft rides on the same internode connection as
//     the rest of the runtime. Wire-byte 0x03 is reserved here so older
//     decoders that did not know about it surface a structured
//     protocolError instead of silently mis-routing the frame.
type Class uint8

const (
	ClassRaftControl Class = iota
	ClassGossip
	ClassPGBroadcast
	ClassRaftMesh
)

// numClasses is the count of Class values. If a new Class is added, this
// MUST be updated; the per-state ring slice is sized from it.
const numClasses = 4

// String renders Class for log/metric labels.
func (c Class) String() string {
	switch c {
	case ClassRaftControl:
		return "raft"
	case ClassGossip:
		return "gossip"
	case ClassPGBroadcast:
		return "pg"
	case ClassRaftMesh:
		return "raft-mesh"
	default:
		return "unknown"
	}
}

// ClassForTopic maps a relay package topic to its QoS class. Membership
// and discovery topics are control-plane (drop-oldest); everything else
// is treated as application broadcast (drop-newest with caller error).
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
