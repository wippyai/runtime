// SPDX-License-Identifier: MPL-2.0
package actor

import (
	"context"
	"encoding/binary"
	"github.com/tetratelabs/wazero/api"
	wasmengine "github.com/wippyai/wasm-runtime/engine"
)

// Register supplies a pre-lift bound check for send. Checking only inside Send
// is too late: a forged list length can allocate enormous Go slices first.
func (h *Host) Register() map[string]any {
	return map[string]any{
		"self":        h.Self,
		"receive":     h.Receive,
		"try-receive": h.TryReceive,
		"send":        wasmengine.CheckedHostFunction{Handler: h.Send, Validate: validateSendArguments},
	}
}

func validateSendArguments(ctx context.Context, mod api.Module, stack []uint64) error {
	m := GetMailbox(ctx)
	if m == nil {
		return ErrActorRequired
	}
	if len(stack) != 7 {
		return ErrInvalidMessage
	}
	targetLen, topicLen, count := uint32(stack[1]), uint32(stack[3]), uint32(stack[5])
	if targetLen > MaxPIDBytes || topicLen > MaxTopicBytes || count > MaxPayloads {
		return ErrTooLarge
	}
	mem := mod.Memory()
	// Canonical payload = {format: string, data: list<u8>}, four u32 fields.
	headers, ok := mem.Read(uint32(stack[4]), count*16)
	if !ok {
		return ErrInvalidMessage
	}
	if _, ok = mem.Read(uint32(stack[0]), targetLen); !ok {
		return ErrInvalidMessage
	}
	if _, ok = mem.Read(uint32(stack[2]), topicLen); !ok {
		return ErrInvalidMessage
	}
	total := int64(topicLen)
	for offset := 0; offset < len(headers); offset += 16 {
		formatPtr := binary.LittleEndian.Uint32(headers[offset:])
		formatLen := binary.LittleEndian.Uint32(headers[offset+4:])
		dataPtr := binary.LittleEndian.Uint32(headers[offset+8:])
		dataLen := binary.LittleEndian.Uint32(headers[offset+12:])
		if formatLen > 5 {
			return ErrUnsupportedPayload
		}
		total += int64(formatLen) + int64(dataLen)
		if total > m.limits.MessageBytes {
			return ErrTooLarge
		}
		if _, ok = mem.Read(formatPtr, formatLen); !ok {
			return ErrInvalidMessage
		}
		if _, ok = mem.Read(dataPtr, dataLen); !ok {
			return ErrInvalidMessage
		}
	}
	return nil
}
