// SPDX-License-Identifier: MPL-2.0

package xmlcmd

import (
	"io"

	"github.com/wippyai/runtime/api/runtime/resource"
	streamapi "github.com/wippyai/runtime/api/stream"
	streamsys "github.com/wippyai/runtime/system/stream"
)

// streamSource reads a table stream, resolving it on every read so that a closed
// stream fails the read instead of reaching released storage.
type streamSource struct {
	table *resource.Table
	id    uint64
}

func (s streamSource) Read(p []byte) (int, error) {
	e, err := streamsys.Get(s.table, s.id)
	if err != nil {
		return 0, err
	}
	r := e.Reader()
	if r == nil {
		return 0, streamapi.ErrNotReadable
	}
	return r.Read(p)
}

// streamSink writes a table stream, resolving it on every write.
type streamSink struct {
	table *resource.Table
	id    uint64
}

func (s streamSink) Write(p []byte) (int, error) {
	return streamsys.Write(s.table, s.id, p)
}

func (s streamSink) flush() error { return streamsys.Flush(s.table, s.id) }

// StreamSource returns a reader over a readable table stream.
func StreamSource(table *resource.Table, id uint64) (io.Reader, error) {
	e, err := streamsys.Get(table, id)
	if err != nil {
		return nil, err
	}
	if !e.Caps().Readable {
		return nil, streamapi.ErrNotReadable
	}
	return streamSource{table: table, id: id}, nil
}

// StreamSink returns a writer over a writable table stream and the function that flushes it.
func StreamSink(table *resource.Table, id uint64) (io.Writer, func() error, error) {
	e, err := streamsys.Get(table, id)
	if err != nil {
		return nil, nil, err
	}
	if !e.Caps().Writable {
		return nil, nil, streamapi.ErrNotWritable
	}
	s := streamSink{table: table, id: id}
	return s, s.flush, nil
}
