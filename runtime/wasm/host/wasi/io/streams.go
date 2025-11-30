// Package io implements wasi:io/streams@0.2.8 for wippy.
// Maps WASI stream operations to wippy's dispatcher stream commands.
package io

import (
	"context"

	"github.com/tetratelabs/wazero/api"

	"github.com/wippyai/runtime/api/dispatcher"
	streamapi "github.com/wippyai/runtime/api/dispatcher/stream"
	wasmapi "github.com/wippyai/runtime/api/runtime/wasm"
	"github.com/wippyai/runtime/runtime/wasm/host"
	"github.com/wippyai/runtime/runtime/wasm/resource"
)

const (
	// StreamsNamespace is the WASI namespace for streams.
	StreamsNamespace = "wasi:io/streams@0.2.8"
)

// StreamsHost implements wasi:io/streams@0.2.8.
type StreamsHost struct {
	resources *resource.InstanceResources
}

// NewStreamsHost creates a new streams host with shared resource table.
func NewStreamsHost(resources *resource.InstanceResources) *StreamsHost {
	return &StreamsHost{
		resources: resources,
	}
}

// Info returns host metadata.
func (h *StreamsHost) Info() wasmapi.HostInfo {
	return wasmapi.HostInfo{
		Namespace:   StreamsNamespace,
		Description: "WASI I/O streams for reading and writing",
		Class:       []string{wasmapi.ClassIO},
	}
}

// Namespace implements wasmrt.Host interface.
func (h *StreamsHost) Namespace() string {
	return StreamsNamespace
}

// Register returns the host registration.
func (h *StreamsHost) Register() *wasmapi.HostRegistration {
	return &wasmapi.HostRegistration{
		Functions: map[string]any{
			// input-stream methods
			"[method]input-stream.read":          host.MakeAsyncHandler(h.makeInputStreamRead),
			"[method]input-stream.blocking-read": host.MakeAsyncHandler(h.makeInputStreamRead),
			"[method]input-stream.skip":          host.MakeAsyncHandler(h.makeInputStreamSkip),
			"[method]input-stream.blocking-skip": host.MakeAsyncHandler(h.makeInputStreamSkip),
			"[resource-drop]input-stream":        h.dropInputStream,

			// output-stream methods
			"[method]output-stream.check-write":                     h.checkWrite,
			"[method]output-stream.write":                           host.MakeAsyncHandler(h.makeOutputStreamWrite),
			"[method]output-stream.blocking-write-and-flush":        host.MakeAsyncHandler(h.makeOutputStreamWrite),
			"[method]output-stream.flush":                           host.MakeAsyncHandler(h.makeOutputStreamFlush),
			"[method]output-stream.blocking-flush":                  host.MakeAsyncHandler(h.makeOutputStreamFlush),
			"[method]output-stream.write-zeroes":                    host.MakeAsyncHandler(h.makeOutputStreamWriteZeroes),
			"[method]output-stream.blocking-write-zeroes-and-flush": host.MakeAsyncHandler(h.makeOutputStreamWriteZeroes),
			"[resource-drop]output-stream":                          h.dropOutputStream,
		},
		YieldTypes: []wasmapi.YieldType{
			{CmdID: streamapi.CmdStreamRead},
			{CmdID: streamapi.CmdStreamWrite},
			{CmdID: streamapi.CmdStreamFlush},
			{CmdID: streamapi.CmdStreamClose},
		},
	}
}

// Resources returns the shared resource table.
func (h *StreamsHost) Resources() *resource.InstanceResources {
	return h.resources
}

// Input stream handlers

func (h *StreamsHost) makeInputStreamRead(stack []uint64) dispatcher.Command {
	var streamID uint64
	var size int64
	if len(stack) > 0 {
		handle := resource.Handle(stack[0])
		if stream, ok := h.resources.InputStreams().Get(handle); ok {
			streamID = stream.StreamID
		}
	}
	if len(stack) > 1 {
		size = int64(stack[1])
	}
	return streamapi.StreamReadCmd{StreamID: streamID, Size: size}
}

func (h *StreamsHost) makeInputStreamSkip(stack []uint64) dispatcher.Command {
	var streamID uint64
	var size int64
	if len(stack) > 0 {
		handle := resource.Handle(stack[0])
		if stream, ok := h.resources.InputStreams().Get(handle); ok {
			streamID = stream.StreamID
		}
	}
	if len(stack) > 1 {
		size = int64(stack[1])
	}
	return streamapi.StreamReadCmd{StreamID: streamID, Size: size}
}

func (h *StreamsHost) dropInputStream(_ context.Context, _ api.Module, stack []uint64) {
	if len(stack) > 0 {
		handle := resource.Handle(stack[0])
		h.resources.Table().Remove(handle)
	}
}

// Output stream handlers

func (h *StreamsHost) checkWrite(_ context.Context, _ api.Module, stack []uint64) {
	// Non-blocking check - return available buffer size
	if len(stack) > 0 {
		stack[0] = 65536 // 64KB default buffer
	}
}

func (h *StreamsHost) makeOutputStreamWrite(stack []uint64) dispatcher.Command {
	var streamID uint64
	if len(stack) > 0 {
		handle := resource.Handle(stack[0])
		if stream, ok := h.resources.OutputStreams().Get(handle); ok {
			streamID = stream.StreamID
		}
	}
	return streamapi.StreamWriteCmd{StreamID: streamID, Data: nil}
}

func (h *StreamsHost) makeOutputStreamFlush(stack []uint64) dispatcher.Command {
	var streamID uint64
	if len(stack) > 0 {
		handle := resource.Handle(stack[0])
		if stream, ok := h.resources.OutputStreams().Get(handle); ok {
			streamID = stream.StreamID
		}
	}
	return streamapi.StreamFlushCmd{StreamID: streamID}
}

func (h *StreamsHost) makeOutputStreamWriteZeroes(stack []uint64) dispatcher.Command {
	var streamID uint64
	if len(stack) > 0 {
		handle := resource.Handle(stack[0])
		if stream, ok := h.resources.OutputStreams().Get(handle); ok {
			streamID = stream.StreamID
		}
	}
	return streamapi.StreamWriteCmd{StreamID: streamID, Data: nil}
}

func (h *StreamsHost) dropOutputStream(_ context.Context, _ api.Module, stack []uint64) {
	if len(stack) > 0 {
		handle := resource.Handle(stack[0])
		h.resources.Table().Remove(handle)
	}
}

// Compile-time check
var _ wasmapi.Host = (*StreamsHost)(nil)
