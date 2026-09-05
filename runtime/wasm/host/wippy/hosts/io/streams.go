// SPDX-License-Identifier: MPL-2.0

package io

import (
	"context"
	"errors"

	wasmengine "github.com/wippyai/wasm-runtime/engine"

	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

const (
	// StreamsNamespace is the WASI IO streams namespace.
	StreamsNamespace = "wasi:io/streams@0.2.8"
)

// StreamsHost exposes wasi:io/streams APIs backed by preview2 resource table.
// Stream resources are created by other hosts (filesystem, HTTP, sockets, etc.)
// and dispatched through interface methods here.
type StreamsHost struct {
	resources *preview2.ResourceTable
}

// NewStreamsHost creates a streams host.
func NewStreamsHost(resources *preview2.ResourceTable) *StreamsHost {
	return &StreamsHost{resources: resources}
}

// Namespace implements wasm-runtime Host.
func (h *StreamsHost) Namespace() string {
	return StreamsNamespace
}

// MethodInputStreamRead reads from an input stream.
func (h *StreamsHost) MethodInputStreamRead(_ context.Context, self uint32, length uint64) ([]byte, *preview2.StreamError) {
	r, ok := h.resources.Get(self)
	if !ok {
		return nil, &preview2.StreamError{Closed: true}
	}

	stream, ok := r.(interface{ Read(uint64) ([]byte, error) })
	if !ok {
		return nil, &preview2.StreamError{Closed: true}
	}

	if length > preview2.MaxAllocationSize {
		return nil, h.operationFailed()
	}

	data, err := stream.Read(length)
	if err != nil {
		var se *preview2.StreamError
		if errors.As(err, &se) {
			return nil, h.streamError(se)
		}
		return nil, h.operationFailed()
	}

	return data, nil
}

// MethodInputStreamBlockingRead reads from an input stream (blocking variant).
func (h *StreamsHost) MethodInputStreamBlockingRead(ctx context.Context, self uint32, length uint64) ([]byte, *preview2.StreamError) {
	data, _, err, handled := h.blockingInput(ctx, self, length, false)
	if handled {
		return data, err
	}
	return h.MethodInputStreamRead(ctx, self, length)
}

// MethodInputStreamSkip skips bytes on an input stream.
func (h *StreamsHost) MethodInputStreamSkip(_ context.Context, self uint32, length uint64) (uint64, *preview2.StreamError) {
	r, ok := h.resources.Get(self)
	if !ok {
		return 0, &preview2.StreamError{Closed: true}
	}

	stream, ok := r.(interface{ Read(uint64) ([]byte, error) })
	if !ok {
		return 0, &preview2.StreamError{Closed: true}
	}

	if length > preview2.MaxAllocationSize {
		return 0, h.operationFailed()
	}

	data, err := stream.Read(length)
	if err != nil {
		var se *preview2.StreamError
		if errors.As(err, &se) {
			return 0, h.streamError(se)
		}
		return 0, h.operationFailed()
	}

	return uint64(len(data)), nil
}

// MethodInputStreamBlockingSkip skips bytes on an input stream (blocking variant).
func (h *StreamsHost) MethodInputStreamBlockingSkip(ctx context.Context, self uint32, length uint64) (uint64, *preview2.StreamError) {
	_, n, err, handled := h.blockingInput(ctx, self, length, true)
	if handled {
		return n, err
	}
	return h.MethodInputStreamSkip(ctx, self, length)
}

// MethodInputStreamSubscribe subscribes to input stream readiness.
func (h *StreamsHost) MethodInputStreamSubscribe(_ context.Context, self uint32) uint32 {
	r, ok := h.resources.Get(self)
	if !ok {
		panic("invalid input stream handle")
	}
	if _, ok := r.(interface{ Read(uint64) ([]byte, error) }); !ok {
		panic("resource is not an input stream")
	}
	if subscriber, ok := r.(interface{ Subscribe() preview2.Pollable }); ok {
		return h.resources.Add(subscriber.Subscribe())
	}
	pollable := &preview2.PollableResource{}
	pollable.SetReady(true)
	return h.resources.Add(pollable)
}

// AsyncFunctions declares blocking input operations for embedded Asyncify.
func (*StreamsHost) AsyncFunctions() []string {
	return []string{"[method]input-stream.blocking-read", "[method]input-stream.blocking-skip", "[method]output-stream.blocking-write-and-flush", "[method]output-stream.blocking-flush", "[method]output-stream.blocking-write-zeroes-and-flush", "[method]output-stream.blocking-splice"}
}

// MethodOutputStreamCheckWrite checks how many bytes can be written.
func (h *StreamsHost) MethodOutputStreamCheckWrite(_ context.Context, self uint32) (uint64, *preview2.StreamError) {
	r, ok := h.resources.Get(self)
	if !ok {
		return 0, &preview2.StreamError{Closed: true}
	}

	stream, ok := r.(interface{ CheckWrite() (uint64, error) })
	if !ok {
		return 1024 * 1024, nil
	}

	size, err := stream.CheckWrite()
	if err != nil {
		var se *preview2.StreamError
		if errors.As(err, &se) {
			return 0, h.streamError(se)
		}
		return 0, h.operationFailed()
	}

	return size, nil
}

// MethodOutputStreamWrite writes data to an output stream.
func (h *StreamsHost) MethodOutputStreamWrite(_ context.Context, self uint32, contents []byte) *preview2.StreamError {
	r, ok := h.resources.Get(self)
	if !ok {
		return &preview2.StreamError{Closed: true}
	}

	stream, ok := r.(interface{ Write([]byte) error })
	if !ok {
		return &preview2.StreamError{Closed: true}
	}

	err := stream.Write(contents)
	if errors.Is(err, preview2.ErrWritePermit) {
		panic(err)
	}
	if err != nil {
		var se *preview2.StreamError
		if errors.As(err, &se) {
			return h.streamError(se)
		}
		return h.operationFailed()
	}

	return nil
}

// MethodOutputStreamBlockingWriteAndFlush writes and flushes (blocking variant).
func (h *StreamsHost) MethodOutputStreamBlockingWriteAndFlush(ctx context.Context, self uint32, contents []byte) *preview2.StreamError {
	if len(contents) > 4096 {
		panic("blocking-write-and-flush exceeds 4096 bytes")
	}
	if err, handled := h.blockingOutput(ctx, self, contents, false); handled {
		return err
	}
	if err := h.MethodOutputStreamWrite(ctx, self, contents); err != nil {
		return err
	}
	return h.MethodOutputStreamFlush(ctx, self)
}

// MethodOutputStreamFlush flushes an output stream.
func (h *StreamsHost) MethodOutputStreamFlush(_ context.Context, self uint32) *preview2.StreamError {
	r, ok := h.resources.Get(self)
	if !ok {
		return &preview2.StreamError{Closed: true}
	}

	if flusher, ok := r.(interface{ Flush() error }); ok {
		if err := flusher.Flush(); err != nil {
			var se *preview2.StreamError
			if errors.As(err, &se) {
				return h.streamError(se)
			}
			return h.operationFailed()
		}
	}
	return nil
}

// MethodOutputStreamBlockingFlush flushes an output stream (blocking variant).
func (h *StreamsHost) MethodOutputStreamBlockingFlush(ctx context.Context, self uint32) *preview2.StreamError {
	if err, handled := h.blockingOutput(ctx, self, nil, true); handled {
		return err
	}
	return h.MethodOutputStreamFlush(ctx, self)
}

// MethodOutputStreamSubscribe subscribes to output stream readiness.
func (h *StreamsHost) MethodOutputStreamSubscribe(_ context.Context, self uint32) uint32 {
	r, ok := h.resources.Get(self)
	if !ok {
		panic("invalid output stream handle")
	}
	if _, ok := r.(interface{ Write([]byte) error }); !ok {
		panic("resource is not an output stream")
	}
	if subscriber, ok := r.(interface{ Subscribe() preview2.Pollable }); ok {
		return h.resources.Add(subscriber.Subscribe())
	}
	pollable := &preview2.PollableResource{}
	pollable.SetReady(true)
	return h.resources.Add(pollable)
}

// MethodOutputStreamWriteZeroes writes zero bytes to an output stream.
func (h *StreamsHost) MethodOutputStreamWriteZeroes(_ context.Context, self uint32, length uint64) *preview2.StreamError {
	r, ok := h.resources.Get(self)
	if !ok {
		return &preview2.StreamError{Closed: true}
	}

	stream, ok := r.(interface{ Write([]byte) error })
	if !ok {
		return &preview2.StreamError{Closed: true}
	}

	if length > preview2.MaxAllocationSize {
		return h.operationFailed()
	}

	if _, ok := r.(*preview2.TCPOutputStreamResource); ok && length > preview2.DefaultBufferSize {
		panic(preview2.ErrWritePermit)
	}
	zeroes := make([]byte, length)
	err := stream.Write(zeroes)
	if errors.Is(err, preview2.ErrWritePermit) {
		panic(err)
	}
	if err != nil {
		var se *preview2.StreamError
		if errors.As(err, &se) {
			return h.streamError(se)
		}
		return h.operationFailed()
	}

	return nil
}

// MethodOutputStreamBlockingWriteZeroesAndFlush writes zeroes and flushes (blocking variant).
func (h *StreamsHost) MethodOutputStreamBlockingWriteZeroesAndFlush(ctx context.Context, self uint32, length uint64) *preview2.StreamError {
	if length > 4096 {
		panic("blocking-write-zeroes-and-flush exceeds 4096 bytes")
	}
	if async := wasmengine.GetAsyncify(ctx); async != nil && async.IsRewinding(ctx) {
		return h.MethodOutputStreamBlockingWriteAndFlush(ctx, self, nil)
	}
	return h.MethodOutputStreamBlockingWriteAndFlush(ctx, self, make([]byte, length))
}

// MethodOutputStreamSplice splices data from input to output stream.
func (h *StreamsHost) MethodOutputStreamSplice(_ context.Context, self uint32, src uint32, length uint64) (uint64, *preview2.StreamError) {
	srcR, ok := h.resources.Get(src)
	if !ok {
		return 0, &preview2.StreamError{Closed: true}
	}
	dstR, ok := h.resources.Get(self)
	if !ok {
		return 0, &preview2.StreamError{Closed: true}
	}
	n, se, _ := spliceOnce(srcR, dstR, length)
	return n, h.streamError(se)
}

// MethodOutputStreamBlockingSplice splices data (blocking variant).
func (h *StreamsHost) MethodOutputStreamBlockingSplice(ctx context.Context, self uint32, src uint32, length uint64) (uint64, *preview2.StreamError) {
	n, err, handled := h.blockingSplice(ctx, self, src, length)
	if handled {
		return n, err
	}
	return h.MethodOutputStreamSplice(ctx, self, src, length)
}

// ResourceDropInputStream drops an input stream resource.
func (h *StreamsHost) ResourceDropInputStream(_ context.Context, self uint32) {
	h.resources.Remove(self)
}

// ResourceDropOutputStream drops an output stream resource.
func (h *StreamsHost) ResourceDropOutputStream(_ context.Context, self uint32) {
	h.resources.Remove(self)
}

// Register returns explicit WIT function mappings.
func (h *StreamsHost) Register() map[string]any {
	return map[string]any{
		"[method]input-stream.read":          h.MethodInputStreamRead,
		"[method]input-stream.blocking-read": h.MethodInputStreamBlockingRead,
		"[method]input-stream.skip":          h.MethodInputStreamSkip,
		"[method]input-stream.blocking-skip": h.MethodInputStreamBlockingSkip,
		"[method]input-stream.subscribe":     h.MethodInputStreamSubscribe,

		"[method]output-stream.check-write":                     h.MethodOutputStreamCheckWrite,
		"[method]output-stream.write":                           wasmengine.CheckedHostFunction{Handler: h.MethodOutputStreamWrite, Validate: validateOutputWrite},
		"[method]output-stream.blocking-write-and-flush":        wasmengine.CheckedHostFunction{Handler: h.MethodOutputStreamBlockingWriteAndFlush, Validate: validateBlockingOutputWrite},
		"[method]output-stream.flush":                           h.MethodOutputStreamFlush,
		"[method]output-stream.blocking-flush":                  h.MethodOutputStreamBlockingFlush,
		"[method]output-stream.subscribe":                       h.MethodOutputStreamSubscribe,
		"[method]output-stream.write-zeroes":                    h.MethodOutputStreamWriteZeroes,
		"[method]output-stream.blocking-write-zeroes-and-flush": h.MethodOutputStreamBlockingWriteZeroesAndFlush,
		"[method]output-stream.splice":                          h.MethodOutputStreamSplice,
		"[method]output-stream.blocking-splice":                 h.MethodOutputStreamBlockingSplice,

		"[resource-drop]input-stream":  h.ResourceDropInputStream,
		"[resource-drop]output-stream": h.ResourceDropOutputStream,
	}
}
