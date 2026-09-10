// SPDX-License-Identifier: MPL-2.0
package stream

import (
	"context"
	"io"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/dispatcher"
)

// PipeAllocator is supplied by trusted host composition in the caller's frame.
// Implementations authorize the actual caller and peer, bind transport identity,
// and tie the returned endpoint to lifetime. Offers are routing metadata,
// never authority to read a producer's files or other resources.
// Allocate must observe ctx cancellation. It runs off the Lua scheduler.
type PipeAllocator interface {
	Allocate(ctx, lifetime context.Context, peer string, limit uint64) (io.ReadCloser, string, error)
}

var pipeAllocatorKey = &ctxapi.Key{Name: "stream.pipe_allocator", Inherit: false}

func SetPipeAllocator(ctx context.Context, a PipeAllocator) error {
	frame := ctxapi.FrameFromContext(ctx)
	if frame == nil {
		return ctxapi.ErrNoFrameContext
	}
	return frame.Set(pipeAllocatorKey, a)
}
func GetPipeAllocator(ctx context.Context) PipeAllocator {
	frame := ctxapi.FrameFromContext(ctx)
	if frame == nil {
		return nil
	}
	v, ok := frame.Get(pipeAllocatorKey)
	if !ok {
		return nil
	}
	a, _ := v.(PipeAllocator)
	return a
}

type PipeCmd struct {
	Peer  string
	Limit uint64
}

func (PipeCmd) CmdID() dispatcher.CommandID { return Pipe }

type PipeResult struct {
	Offer    string
	StreamID uint64
}
