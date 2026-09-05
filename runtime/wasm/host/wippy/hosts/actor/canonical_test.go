// SPDX-License-Identifier: MPL-2.0
package actor

import (
	"context"
	"encoding/binary"
	"github.com/tetratelabs/wazero"
	"github.com/wippyai/wasm-runtime/wat"
	"testing"
)

func TestCanonicalSendPreflight(t *testing.T) {
	ctx := WithMailbox(context.Background(), NewMailbox(DefaultLimits()))
	rt := wazero.NewRuntime(ctx)
	defer rt.Close(ctx)
	b, err := wat.Compile(`(module (memory (export "memory") 1))`)
	if err != nil {
		t.Fatal(err)
	}
	mod, err := rt.Instantiate(ctx, b)
	if err != nil {
		t.Fatal(err)
	}
	mem := mod.Memory()
	mem.Write(32, []byte("bytes"))
	mem.Write(40, []byte("hello"))
	header := make([]byte, 16)
	for i, v := range []uint32{32, 5, 40, 5} {
		binary.LittleEndian.PutUint32(header[i*4:], v)
	}
	mem.Write(64, header)
	valid := []uint64{0, 1, 0, 1, 64, 1, 100}
	for _, tc := range []struct {
		name   string
		mutate func([]uint64)
		bad    bool
	}{
		{"valid", func([]uint64) {}, false},
		{"huge payload count", func(s []uint64) { s[5] = 1 << 27 }, true},
		{"huge target", func(s []uint64) { s[1] = 1 << 30 }, true},
		{"huge topic", func(s []uint64) { s[3] = 1 << 30 }, true},
		{"invalid headers", func(s []uint64) { s[4] = 65532 }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := append([]uint64(nil), valid...)
			tc.mutate(s)
			if err := validateSendArguments(ctx, mod, s); (err != nil) != tc.bad {
				t.Fatalf("error=%v", err)
			}
		})
	}
	binary.LittleEndian.PutUint32(header[12:], 1<<30)
	mem.Write(64, header)
	if err := validateSendArguments(ctx, mod, valid); err != ErrTooLarge {
		t.Fatalf("oversized nested data: %v", err)
	}
}
