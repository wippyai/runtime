// SPDX-License-Identifier: MPL-2.0

package cdc

import (
	"sync"

	"github.com/wippyai/runtime/api/registry"
	api "github.com/wippyai/runtime/api/service/cdc"
)

const (
	defaultStreamBuffer = 128
	maxStreamBuffer     = 65536
)

// stampedStream is the boundary between a driver stream and the common CDC
// API. Drivers own transport-specific change decoding; the stable source slot
// owns the process identity and generation that consumers use for routing and
// resume diagnostics.
type stampedStream struct {
	upstream   api.Stream
	sourceID   registry.ID
	generation string
	out        chan api.Change
	done       chan struct{}
	once       sync.Once
}

func newStampedStream(id registry.ID, generation uint64, requestedBuffer int, upstream api.Stream) *stampedStream {
	buffer := requestedBuffer
	if buffer <= 0 {
		buffer = defaultStreamBuffer
	}
	if buffer > maxStreamBuffer {
		buffer = maxStreamBuffer
	}

	stream := &stampedStream{
		upstream:   upstream,
		sourceID:   registry.ParseID(id.String()),
		generation: generationString(generation),
		out:        make(chan api.Change, buffer),
		done:       make(chan struct{}),
	}
	go stream.run()
	return stream
}

func (s *stampedStream) Changes() <-chan api.Change { return s.out }

func (s *stampedStream) Close() {
	s.once.Do(func() {
		close(s.done)
		s.upstream.Close()
	})
}

func (s *stampedStream) Err() error {
	return s.upstream.Err()
}

func (s *stampedStream) run() {
	defer close(s.out)
	changes := s.upstream.Changes()
	for {
		select {
		case <-s.done:
			return
		case change, ok := <-changes:
			if !ok {
				return
			}
			change.SourceID = s.sourceID
			change.Source = s.sourceID.String()
			change.Generation = s.generation
			select {
			case <-s.done:
				return
			case s.out <- change:
			}
		}
	}
}

var _ api.Stream = (*stampedStream)(nil)
var _ api.ErrStream = (*stampedStream)(nil)
