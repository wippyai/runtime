// SPDX-License-Identifier: MPL-2.0

// Package kvraft implements a strongly-consistent cluster key-value store
// backed by its own hashicorp/raft replication group. The FSM holds a
// sharded in-memory map of (space, key) → entry; raft logs are persisted
// via raft-boltdb/v2.
//
// One Service instance hosts multiple named "spaces" (namespaces). All
// spaces share a single FSM and one raft replication group — the namespace
// is encoded in the command so reads/writes don't cross.
package kvraft

import (
	"github.com/hashicorp/go-msgpack/v2/codec"
)

// CmdType identifies the operation to apply to the FSM.
type CmdType uint8

const (
	// CmdPut writes/updates a key. If WithExpectVersion or WithExpectAbsent
	// is set on the originating call, the FSM checks pre-conditions.
	CmdPut CmdType = 1
	// CmdDelete removes a key.
	CmdDelete CmdType = 2
	// CmdCAS atomically swaps `Value` only if the current value bytes
	// equal `ExpectValue`. Returns ErrCASMismatch otherwise.
	CmdCAS CmdType = 3
	// CmdReapTTL removes any keys whose TTL has elapsed. Idempotent — every
	// follower reaps the same set on Apply.
	CmdReapTTL CmdType = 4
)

// Command is the unit of mutation applied to the FSM via Raft.
type Command struct {
	Space         string  `codec:"s,omitempty"`
	Key           string  `codec:"k,omitempty"`
	Value         []byte  `codec:"v,omitempty"`
	ExpectValue   []byte  `codec:"e,omitempty"`
	TTL           int64   `codec:"t,omitempty"` // ms epoch; 0 = no TTL
	ExpectVersion uint64  `codec:"x,omitempty"` // 0 == no expectation
	ExpectAbsent  bool    `codec:"a,omitempty"`
	Type          CmdType `codec:"y"`
}

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

// DecodeCommand reverses EncodeCommand.
func DecodeCommand(data []byte) (*Command, error) {
	var cmd Command
	dec := codec.NewDecoderBytes(data, newMsgpackHandle())
	if err := dec.Decode(&cmd); err != nil {
		return nil, err
	}
	return &cmd, nil
}

// Result is the FSM Apply response.
type Result struct {
	// Err is non-nil when the apply failed at the FSM level (CAS mismatch,
	// version mismatch, key-exists, etc.). Surfaces back to the caller via
	// the raft Apply round-trip.
	Err error
	// Version is the raft log index of the apply. Callers use it as the
	// `Version` returned via kv.Value.
	Version uint64
	// Removed is set by CmdReapTTL — the count of keys reaped.
	Removed int
}
