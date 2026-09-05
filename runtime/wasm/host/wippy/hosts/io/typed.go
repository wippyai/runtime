// SPDX-License-Identifier: MPL-2.0

package io

import (
	"context"

	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

func asHostError(err *preview2.StreamError) error {
	if err != nil {
		return err
	}
	return nil
}

func (h *StreamsHost) typedInputStreamRead(ctx context.Context, self uint32, length uint64) ([]byte, error) {
	data, err := h.MethodInputStreamRead(ctx, self, length)
	return data, asHostError(err)
}

func (h *StreamsHost) typedInputStreamSkip(ctx context.Context, self uint32, length uint64) (uint64, error) {
	n, err := h.MethodInputStreamSkip(ctx, self, length)
	return n, asHostError(err)
}

func (h *StreamsHost) typedOutputStreamCheckWrite(ctx context.Context, self uint32) (uint64, error) {
	n, err := h.MethodOutputStreamCheckWrite(ctx, self)
	return n, asHostError(err)
}

func (h *StreamsHost) typedOutputStreamWrite(ctx context.Context, self uint32, contents []byte) (struct{}, error) {
	return struct{}{}, asHostError(h.MethodOutputStreamWrite(ctx, self, contents))
}

func (h *StreamsHost) typedOutputStreamFlush(ctx context.Context, self uint32) (struct{}, error) {
	return struct{}{}, asHostError(h.MethodOutputStreamFlush(ctx, self))
}
