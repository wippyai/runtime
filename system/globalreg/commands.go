// SPDX-License-Identifier: MPL-2.0

package globalreg

import (
	"github.com/hashicorp/go-msgpack/v2/codec"
	"github.com/wippyai/runtime/api/pid"
)

// CommandType identifies the operation to apply to the FSM.
type CommandType uint8

const (
	// CmdRegister registers a name → PID mapping at Consistent scope.
	CmdRegister CommandType = 1
	// CmdUnregister removes a name registration.
	CmdUnregister CommandType = 2
	// CmdRemovePID removes all names for a specific PID (process exit).
	CmdRemovePID CommandType = 3
	// CmdRemoveNode removes all names for all PIDs on a node (node failure).
	CmdRemoveNode CommandType = 4

	// CmdRegisterPending opens a Root-scope reservation. The name is
	// reserved (no other pid may overwrite) but not authoritative until
	// every node in RequiredNodes acks the committed epoch.
	CmdRegisterPending CommandType = 5
	// CmdRegisterAck records that a single node has acked the pending
	// reservation. When the ack set covers RequiredNodes the FSM
	// promotes the pending entry to active in the same Apply call —
	// this keeps the transition atomic across all replicas.
	CmdRegisterAck CommandType = 6
	// CmdRegisterExpired releases a pending reservation whose deadline
	// elapsed before every required node acked.
	CmdRegisterExpired CommandType = 7
	// CmdRegisterUnreserve removes a pending or active Root entry on
	// explicit caller request (Service.UnregisterScope).
	CmdRegisterUnreserve CommandType = 8
)

// Command is the unit of mutation applied to the FSM via Raft.
type Command struct {
	PID              pid.PID      `codec:"p,omitempty" json:"pid,omitempty"`
	Name             string       `codec:"n,omitempty" json:"name,omitempty"`
	NodeID           pid.NodeID   `codec:"d,omitempty" json:"node_id,omitempty"`
	AckerNode        pid.NodeID   `codec:"ak,omitempty" json:"acker_node,omitempty"`
	Reason           string       `codec:"rs,omitempty" json:"reason,omitempty"`
	RequiredNodes    []pid.NodeID `codec:"r,omitempty" json:"required_nodes,omitempty"`
	Epoch            uint64       `codec:"e,omitempty" json:"epoch,omitempty"`
	DeadlineUnixNano int64        `codec:"dl,omitempty" json:"deadline_unix_nano,omitempty"`
	Limit            int          `codec:"l,omitempty" json:"limit,omitempty"`
	Type             CommandType  `codec:"t" json:"type"`
}

// newMsgpackHandle creates a new MsgpackHandle per operation to avoid
// concurrent use of shared encoder/decoder state.
func newMsgpackHandle() *codec.MsgpackHandle {
	return &codec.MsgpackHandle{}
}

// EncodeCommand serializes a Command to msgpack bytes.
func EncodeCommand(cmd *Command) ([]byte, error) {
	var buf []byte
	enc := codec.NewEncoderBytes(&buf, newMsgpackHandle())
	if err := enc.Encode(cmd); err != nil {
		return nil, err
	}
	return buf, nil
}

// DecodeCommand deserialises msgpack bytes into a Command.
func DecodeCommand(data []byte) (*Command, error) {
	var cmd Command
	dec := codec.NewDecoderBytes(data, newMsgpackHandle())
	if err := dec.Decode(&cmd); err != nil {
		return nil, err
	}
	return &cmd, nil
}
