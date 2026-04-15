// SPDX-License-Identifier: MPL-2.0

package globalreg

import (
	"github.com/hashicorp/go-msgpack/v2/codec"
	"github.com/wippyai/runtime/api/pid"
)

// CommandType identifies the operation to apply to the FSM.
type CommandType uint8

const (
	// CmdRegister registers a name → PID mapping.
	CmdRegister CommandType = 1
	// CmdUnregister removes a name registration.
	CmdUnregister CommandType = 2
	// CmdRemovePID removes all names for a specific PID (process exit).
	CmdRemovePID CommandType = 3
	// CmdRemoveNode removes all names for all PIDs on a node (node failure).
	CmdRemoveNode CommandType = 4
)

// Command is the unit of mutation applied to the FSM via Raft.
type Command struct {
	PID    pid.PID     `codec:"p,omitempty" json:"pid,omitempty"`
	Name   string      `codec:"n,omitempty" json:"name,omitempty"`
	NodeID pid.NodeID  `codec:"d,omitempty" json:"node_id,omitempty"`
	Type   CommandType `codec:"t" json:"type"`
}

// handle is the msgpack codec handle, shared for all encode/decode operations.
var handle = &codec.MsgpackHandle{}

// EncodeCommand serializes a Command to msgpack bytes.
func EncodeCommand(cmd *Command) ([]byte, error) {
	var buf []byte
	enc := codec.NewEncoderBytes(&buf, handle)
	if err := enc.Encode(cmd); err != nil {
		return nil, err
	}
	return buf, nil
}

// DecodeCommand deserialises msgpack bytes into a Command.
func DecodeCommand(data []byte) (*Command, error) {
	var cmd Command
	dec := codec.NewDecoderBytes(data, handle)
	if err := dec.Decode(&cmd); err != nil {
		return nil, err
	}
	return &cmd, nil
}
