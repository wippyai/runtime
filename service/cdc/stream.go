// SPDX-License-Identifier: MPL-2.0

package cdc

import (
	"sync"

	"github.com/wippyai/runtime/api/registry"
	api "github.com/wippyai/runtime/api/service/cdc"
)

// stampedStream is the boundary between a driver stream and the common CDC
// API. Drivers own transport-specific change decoding; the stable source slot
// owns the process identity and generation that consumers use for routing and
// resume diagnostics.
type stampedStream struct {
	release    func()
	upstream   api.Stream
	out        chan api.Change
	done       chan struct{}
	sourceID   registry.ID
	generation string
	once       sync.Once
}

func newStampedStream(id registry.ID, generation uint64, upstream api.Stream, release func()) *stampedStream {
	stream := &stampedStream{
		release:    release,
		upstream:   upstream,
		sourceID:   registry.ParseID(id.String()),
		generation: generationString(generation),
		// The driver owns the only bounded subscriber queue. Keep this common
		// identity adapter unbuffered so it cannot double retained events.
		out:  make(chan api.Change),
		done: make(chan struct{}),
	}
	go stream.run()
	return stream
}

func (s *stampedStream) Changes() <-chan api.Change { return s.out }

func (s *stampedStream) Close() {
	s.once.Do(func() {
		close(s.done)
		s.upstream.Close()
		if s.release != nil {
			s.release()
		}
	})
}

func (s *stampedStream) Err() error {
	return s.upstream.Err()
}

func (s *stampedStream) run() {
	defer close(s.out)
	defer s.Close()
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
