// SPDX-License-Identifier: MPL-2.0

package globalreg

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/hashicorp/go-msgpack/v2/codec"

	"github.com/wippyai/runtime/api/globalreg"
	"github.com/wippyai/runtime/api/pid"
)

// forwardWireVersion is the version byte used by v1 forward-response envelopes.
const forwardWireVersion = 1

// forwardWireMagic is the discriminator byte placed at offset 8 of a v1
// envelope. v0 envelopes carry an error string in the same position, which is
// always non-zero printable ASCII (Go's fmt.Errorf / errors.New cannot return
// a string starting with a NUL byte), so this magic byte is unambiguous.
const forwardWireMagic = 0x00

// commandLabel returns a low-cardinality label for prometheus.
// It must stay stable across releases; existing dashboards depend on it.
func commandLabel(t CommandType) string {
	switch t {
	case CmdRegister:
		return "register"
	case CmdUnregister:
		return "unregister"
	case CmdRemovePID:
		return "remove_pid"
	case CmdRemoveNode:
		return "remove_node"
	case CmdRegisterPending:
		return "register_pending"
	case CmdRegisterAck:
		return "register_ack"
	case CmdRegisterExpired:
		return "register_expired"
	case CmdRegisterUnreserve:
		return "register_unreserve"
	case CmdDropRequired:
		return "drop_required"
	case CmdRegisterReject:
		return "register_reject"
	default:
		return "unknown"
	}
}

// forwardBody is the msgpack-encoded body of a v1 forward-response envelope.
// It is intentionally simple: a single command-kind discriminator, an optional
// error string (mirrors v0 semantics), and an opaque result blob whose shape
// is determined by CmdKind.
type forwardBody struct {
	Err     string      `codec:"e,omitempty"`
	Result  []byte      `codec:"r,omitempty"`
	CmdKind CommandType `codec:"k"`
}

// wireRegisterResult mirrors RegisterResult but uses a string for the error
// field so the result is round-trippable through msgpack. The boundary code
// is responsible for converting between the wire form and the public form.
type wireRegisterResult struct {
	Err         string  `codec:"e,omitempty"`
	PID         pid.PID `codec:"p,omitempty"`
	ExistingPID pid.PID `codec:"x,omitempty"`
	ResolvedPID pid.PID `codec:"r,omitempty"`
	FenceToken  uint64  `codec:"f,omitempty"`
	State       uint8   `codec:"s,omitempty"`
}

type wireUnregisterResult struct {
	Removed bool `codec:"d"`
}

type wireRemoveResult struct {
	Count   int  `codec:"c"`
	HasMore bool `codec:"m,omitempty"`
}

type wireAckResult struct {
	Recognized bool `codec:"r"`
	Complete   bool `codec:"c,omitempty"`
	Activated  bool `codec:"a,omitempty"`
}

type wireExpireResult struct {
	MissingAcks []pid.NodeID `codec:"m,omitempty"`
	Removed     bool         `codec:"d"`
}

type wireRejectResult struct {
	RejectedBy pid.NodeID `codec:"b,omitempty"`
	Rejected   bool       `codec:"r"`
}

// encodeFSMResult serializes an FSM apply response so it can be sent back to a
// forwarding follower. It returns the encoded bytes and the matching command
// kind tag for the envelope header.
//
// If result is nil, the function returns (nil, nil) — a v0-equivalent empty
// result. Callers that need to distinguish "no result" from "encode error"
// should inspect the error.
func encodeFSMResult(cmd CommandType, result any) ([]byte, error) {
	switch v := result.(type) {
	case nil:
		return nil, nil
	case *RegisterResult:
		wr := wireRegisterResult{
			PID:         v.PID,
			ExistingPID: v.ExistingPID,
			ResolvedPID: v.ResolvedPID,
			FenceToken:  v.FenceToken,
			State:       uint8(v.State),
		}
		if v.Err != nil {
			wr.Err = v.Err.Error()
		}
		return marshalMsgpack(wr)
	case *UnregisterResult:
		return marshalMsgpack(wireUnregisterResult{Removed: v.Removed})
	case *RemoveResult:
		return marshalMsgpack(wireRemoveResult{Count: v.Count, HasMore: v.HasMore})
	case *AckResult:
		return marshalMsgpack(wireAckResult{Recognized: v.Recognized, Complete: v.Complete, Activated: v.Activated})
	case *ExpireResult:
		return marshalMsgpack(wireExpireResult{Removed: v.Removed, MissingAcks: v.MissingAcks})
	case *RejectResult:
		return marshalMsgpack(wireRejectResult{Rejected: v.Rejected, RejectedBy: v.RejectedBy})
	case error:
		return nil, fmt.Errorf("encodeFSMResult: result is an error (cmd=%s); the caller must route errors via the envelope's Err field, not Result bytes", commandLabel(cmd))
	default:
		return nil, fmt.Errorf("encodeFSMResult: unsupported result type %T for cmd=%s", result, commandLabel(cmd))
	}
}

// decodeFSMResult is the inverse of encodeFSMResult. It returns the typed
// pointer (e.g. *RegisterResult) that the original FSM apply produced.
func decodeFSMResult(cmd CommandType, data []byte) (any, error) {
	if len(data) == 0 {
		return nil, nil
	}
	switch cmd {
	case CmdRegister, CmdRegisterPending:
		var wr wireRegisterResult
		if err := unmarshalMsgpack(data, &wr); err != nil {
			return nil, fmt.Errorf("decode register result: %w", err)
		}
		out := &RegisterResult{
			PID:         wr.PID,
			ExistingPID: wr.ExistingPID,
			ResolvedPID: wr.ResolvedPID,
			FenceToken:  wr.FenceToken,
			State:       globalreg.RegisterState(wr.State),
		}
		if wr.Err != "" {
			out.Err = errors.New(wr.Err)
		}
		return out, nil
	case CmdUnregister, CmdRegisterUnreserve:
		var wr wireUnregisterResult
		if err := unmarshalMsgpack(data, &wr); err != nil {
			return nil, fmt.Errorf("decode unregister result: %w", err)
		}
		return &UnregisterResult{Removed: wr.Removed}, nil
	case CmdRemovePID, CmdRemoveNode:
		var wr wireRemoveResult
		if err := unmarshalMsgpack(data, &wr); err != nil {
			return nil, fmt.Errorf("decode remove result: %w", err)
		}
		return &RemoveResult{Count: wr.Count, HasMore: wr.HasMore}, nil
	case CmdRegisterAck:
		var wr wireAckResult
		if err := unmarshalMsgpack(data, &wr); err != nil {
			return nil, fmt.Errorf("decode ack result: %w", err)
		}
		return &AckResult{Recognized: wr.Recognized, Complete: wr.Complete, Activated: wr.Activated}, nil
	case CmdRegisterExpired:
		var wr wireExpireResult
		if err := unmarshalMsgpack(data, &wr); err != nil {
			return nil, fmt.Errorf("decode expire result: %w", err)
		}
		return &ExpireResult{Removed: wr.Removed, MissingAcks: wr.MissingAcks}, nil
	case CmdRegisterReject:
		var wr wireRejectResult
		if err := unmarshalMsgpack(data, &wr); err != nil {
			return nil, fmt.Errorf("decode reject result: %w", err)
		}
		return &RejectResult{Rejected: wr.Rejected, RejectedBy: wr.RejectedBy}, nil
	default:
		return nil, fmt.Errorf("decode result: unknown command kind %d", cmd)
	}
}

// encodeForwardResponse builds a v1 response envelope:
//
//	[8B corrID][1B magic=0x00][1B version=1][msgpack(forwardBody)]
//
// A response carrying only an error (no typed result) is still encoded as v1
// so the receiver can reliably tell the two cases apart and observe the
// command kind on the receiving side.
func encodeForwardResponse(corrID uint64, cmd CommandType, errMsg string, result []byte) ([]byte, error) {
	body := forwardBody{
		CmdKind: cmd,
		Err:     errMsg,
		Result:  result,
	}
	encoded, err := marshalMsgpack(body)
	if err != nil {
		return nil, fmt.Errorf("encode forward response body: %w", err)
	}
	out := make([]byte, 8+2+len(encoded))
	binary.BigEndian.PutUint64(out[:8], corrID)
	out[8] = forwardWireMagic
	out[9] = forwardWireVersion
	copy(out[10:], encoded)
	return out, nil
}

// decodedForwardResponse is the parsed representation of an envelope coming
// back from the leader. ErrMsg is mutually exclusive with a valid Result.
type decodedForwardResponse struct {
	Result  any
	ErrMsg  string
	CorrID  uint64
	CmdKind CommandType
	V1      bool
}

// decodeForwardResponse parses an envelope produced by either an old (v0) or a
// new (v1) leader. Callers must already have ensured len(envelope) >= 8.
//
// v0 wire format (legacy): [8B corrID][errMsg bytes]
// v1 wire format (new):    [8B corrID][0x00][0x01][msgpack(forwardBody)]
//
// On v0 input the returned CmdKind is zero and Result is nil. On v1 input the
// Result field carries the typed pointer (e.g. *RegisterResult) when the
// command applied successfully.
func decodeForwardResponse(envelope []byte) (decodedForwardResponse, error) {
	if len(envelope) < 8 {
		return decodedForwardResponse{}, fmt.Errorf("forward response too short: %d bytes", len(envelope))
	}
	out := decodedForwardResponse{
		CorrID: binary.BigEndian.Uint64(envelope[:8]),
	}
	// v0: nothing after the corrID, or first byte after corrID is non-zero
	// (= start of a printable error string).
	if len(envelope) == 8 {
		return out, nil
	}
	if envelope[8] != forwardWireMagic {
		out.ErrMsg = string(envelope[8:])
		return out, nil
	}
	// v1: require at least the version byte.
	if len(envelope) < 10 {
		return out, fmt.Errorf("forward response v1 header truncated: %d bytes", len(envelope))
	}
	if envelope[9] != forwardWireVersion {
		return out, fmt.Errorf("forward response: unsupported version %d", envelope[9])
	}
	out.V1 = true
	var body forwardBody
	if err := unmarshalMsgpack(envelope[10:], &body); err != nil {
		return out, fmt.Errorf("decode forward body: %w", err)
	}
	out.CmdKind = body.CmdKind
	out.ErrMsg = body.Err
	if len(body.Result) > 0 {
		decoded, err := decodeFSMResult(body.CmdKind, body.Result)
		if err != nil {
			return out, err
		}
		out.Result = decoded
	}
	return out, nil
}

func marshalMsgpack(v any) ([]byte, error) {
	var buf []byte
	enc := codec.NewEncoderBytes(&buf, newMsgpackHandle())
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return buf, nil
}

func unmarshalMsgpack(data []byte, v any) error {
	dec := codec.NewDecoderBytes(data, newMsgpackHandle())
	return dec.Decode(v)
}
