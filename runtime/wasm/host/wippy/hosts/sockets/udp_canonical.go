// SPDX-License-Identifier: MPL-2.0
package sockets

import (
	"context"
	"encoding/binary"
	"errors"

	"github.com/tetratelabs/wazero/api"
)

const maxUDPBatch = 16
const maxUDPDatagramBytes = 65535
const maxUDPBatchBytes = maxUDPBatch * maxUDPDatagramBytes

// Canonical wasm32 outgoing-datagram is {data: list<u8>, remote-address:
// option<ip-socket-address>}. The list occupies 8 bytes; the option occupies
// 36 bytes (4-byte tag area and 32-byte socket address), aligned to 4.
const outgoingDatagramStride = 44

// Bound both nested payloads and the outer list before canonical lifting copies
// attacker-controlled guest memory into Go allocations.
func validateUDPDatagrams(_ context.Context, mod api.Module, stack []uint64) error {
	if len(stack) != 4 || uint32(stack[2]) > maxUDPBatch {
		return errors.New("UDP send exceeds datagram batch limit")
	}
	if mod == nil || mod.Memory() == nil {
		return errors.New("UDP send memory unavailable")
	}
	mem := mod.Memory()
	headers, ok := mem.Read(uint32(stack[1]), uint32(stack[2])*outgoingDatagramStride)
	if !ok {
		return errors.New("UDP datagram list outside memory")
	}
	var bytes uint64
	for offset := 0; offset < len(headers); offset += outgoingDatagramStride {
		ptr := binary.LittleEndian.Uint32(headers[offset:])
		size := binary.LittleEndian.Uint32(headers[offset+4:])
		bytes += uint64(size)
		if bytes > maxUDPBatchBytes {
			return errors.New("UDP send exceeds host byte budget")
		}
		if _, ok := mem.Read(ptr, size); !ok {
			return errors.New("UDP datagram data outside memory")
		}
	}
	return nil
}
