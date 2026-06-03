// SPDX-License-Identifier: MPL-2.0

package kv

import (
	"encoding/binary"
	"fmt"

	kvapi "github.com/wippyai/runtime/api/store/kv"
)

// opcode identifies a replicated kv mutation. Encoded as the first byte of a
// command, which the raft FSM applies deterministically on every node.
type opcode uint8

const (
	opSet opcode = iota + 1
	opDelete
	opCAS
	opSetIfAbsent
	opSetWithLease
	opSetIfAbsentWithLease
	opLeaseGrant
	opLeaseRenew
	opLeaseRevoke
	opCompareAndDelete
	opTxn
)

// command is a single replicated mutation. Not all fields apply to every op;
// the codec writes them unconditionally for a fixed, deterministic layout.
type command struct {
	Key     string
	LeaseID kvapi.LeaseID
	Value   []byte
	Expect  kvapi.Version
	TTLms   int64
	Op      opcode
}

// encodeCommand serializes a command with a compact length-prefixed layout:
//
//	op:1 | keyLen:4 | key | valLen:4 | val | expect:8 | leaseLen:4 | lease | ttl:8
func encodeCommand(c command) []byte {
	buf := make([]byte, 0, 1+4+len(c.Key)+4+len(c.Value)+8+4+len(c.LeaseID)+8)
	buf = append(buf, byte(c.Op))
	buf = appendBytes(buf, []byte(c.Key))
	buf = appendBytes(buf, c.Value)
	buf = binary.BigEndian.AppendUint64(buf, c.Expect)
	buf = appendBytes(buf, []byte(c.LeaseID))
	buf = binary.BigEndian.AppendUint64(buf, uint64(c.TTLms))
	return buf
}

// decodeCommand reverses encodeCommand.
func decodeCommand(data []byte) (command, error) {
	var c command
	if len(data) < 1 {
		return c, fmt.Errorf("kv command: empty")
	}
	c.Op = opcode(data[0])
	off := 1

	key, off, err := readBytes(data, off)
	if err != nil {
		return c, err
	}
	c.Key = string(key)

	val, off, err := readBytes(data, off)
	if err != nil {
		return c, err
	}
	c.Value = val

	if off+8 > len(data) {
		return c, fmt.Errorf("kv command: truncated expect")
	}
	c.Expect = binary.BigEndian.Uint64(data[off : off+8])
	off += 8

	lease, off, err := readBytes(data, off)
	if err != nil {
		return c, err
	}
	c.LeaseID = kvapi.LeaseID(lease)

	if off+8 > len(data) {
		return c, fmt.Errorf("kv command: truncated ttl")
	}
	c.TTLms = int64(binary.BigEndian.Uint64(data[off : off+8]))
	return c, nil
}

// encodeTxn serializes a transaction command with layout:
//
//	op:1(opTxn) | count:4 | repeated{ kind:1 | cond:1 | keyLen:4|key | valLen:4|val | expect:8 }
func encodeTxn(ops []kvapi.TxnOp) []byte {
	buf := make([]byte, 0, 1+4+len(ops)*32)
	buf = append(buf, byte(opTxn))
	buf = binary.BigEndian.AppendUint32(buf, uint32(len(ops)))
	for _, op := range ops {
		buf = append(buf, byte(op.Kind), byte(op.Cond))
		buf = appendBytes(buf, []byte(op.Key))
		buf = appendBytes(buf, op.Value)
		buf = binary.BigEndian.AppendUint64(buf, op.Expect)
	}
	return buf
}

// decodeTxn reverses encodeTxn.
func decodeTxn(data []byte) ([]kvapi.TxnOp, error) {
	if len(data) < 5 || opcode(data[0]) != opTxn {
		return nil, fmt.Errorf("kv txn: bad header")
	}
	n := int(binary.BigEndian.Uint32(data[1:5]))
	off := 5
	ops := make([]kvapi.TxnOp, 0, n)
	for i := 0; i < n; i++ {
		if off+2 > len(data) {
			return nil, fmt.Errorf("kv txn: truncated op header at %d", off)
		}
		op := kvapi.TxnOp{Kind: kvapi.TxnOpKind(data[off]), Cond: kvapi.TxnCond(data[off+1])}
		off += 2

		key, koff, err := readBytes(data, off)
		if err != nil {
			return nil, err
		}
		off = koff
		op.Key = string(key)

		val, voff, err := readBytes(data, off)
		if err != nil {
			return nil, err
		}
		off = voff
		op.Value = val

		if off+8 > len(data) {
			return nil, fmt.Errorf("kv txn: truncated expect at %d", off)
		}
		op.Expect = binary.BigEndian.Uint64(data[off : off+8])
		off += 8
		ops = append(ops, op)
	}
	return ops, nil
}

func appendBytes(buf, b []byte) []byte {
	buf = binary.BigEndian.AppendUint32(buf, uint32(len(b)))
	return append(buf, b...)
}

func readBytes(data []byte, off int) ([]byte, int, error) {
	if off+4 > len(data) {
		return nil, off, fmt.Errorf("kv command: truncated length at %d", off)
	}
	n := int(binary.BigEndian.Uint32(data[off : off+4]))
	off += 4
	if off+n > len(data) {
		return nil, off, fmt.Errorf("kv command: truncated payload at %d (want %d)", off, n)
	}
	out := make([]byte, n)
	copy(out, data[off:off+n])
	return out, off + n, nil
}

// applyResult is the FSM's response to an applied command, returned to the
// proposing engine via the raft Apply future.
type applyResult struct {
	Err     error
	Version kvapi.Version
	OK      bool
}
