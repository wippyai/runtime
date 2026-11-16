// SPDX-License-Identifier: MPL-2.0

// Package xmlcmd holds XML reader and writer resources and executes their stream commands.
package xmlcmd

import (
	"context"
	"io"
	"sync"
	"sync/atomic"

	"github.com/wippyai/runtime/api/runtime/resource"
	xmlapi "github.com/wippyai/runtime/api/xml"
	"github.com/wippyai/runtime/system/xml"
)

// TypeReader is the resource table type ID for XML readers.
const TypeReader uint32 = 0x12

// TypeWriter is the resource table type ID for XML writers.
const TypeWriter uint32 = 0x13

// CursorBudget bounds the bytes of events a reader reads ahead.
const CursorBudget = 64 << 10

// Reader owns a decoder, its cursor and the batches that carry results to the VM.
// Operations serialize on the reader; Drop never blocks, so a table reset cannot
// deadlock with an operation that is reading the source.
type Reader struct {
	dec      *xml.Decoder
	cursor   *xml.Cursor
	selector *xml.Selector
	node     xml.Batch
	records  xml.Batch
	mu       sync.Mutex
	closed   atomic.Bool
}

// NewReader creates a reader over src.
func NewReader(ctx context.Context, src io.Reader, opts xml.Options) (*Reader, error) {
	dec, err := xml.NewDecoder(ctx, src, opts)
	if err != nil {
		return nil, err
	}
	cursor, err := xml.NewCursor(dec, CursorBudget)
	if err != nil {
		return nil, err
	}
	return &Reader{dec: dec, cursor: cursor}, nil
}

// Cursor returns the event cursor. Callers use it only between operations.
func (r *Reader) Cursor() *xml.Cursor { return r.cursor }

// ViewNode passes the batch filled by Node to fn under the reader's lock.
func (r *Reader) ViewNode(fn func(*xml.Batch) error) error {
	return r.locked(func() error { return fn(&r.node) })
}

// ViewRecords passes the batch filled by ReadRecords to fn under the reader's lock.
func (r *Reader) ViewRecords(fn func(*xml.Batch) error) error {
	return r.locked(func() error { return fn(&r.records) })
}

// Selected reports whether record selection owns the reader.
func (r *Reader) Selected() bool { return r.selector != nil }

// Err returns the failure that made the reader or its selection terminal, or nil. It
// reads only state that release leaves in place, so it is valid after Close.
func (r *Reader) Err() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.selector != nil {
		if err := r.selector.Err(); err != nil {
			return err
		}
	}
	return r.cursor.Err()
}

// Abort makes the reader terminal with err, a failure that ended an operation outside it.
func (r *Reader) Abort(err error) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.cursor.Abort(err)
}

// Closed reports whether the reader has been released.
func (r *Reader) Closed() bool { return r.closed.Load() }

// locked runs op holding the reader's lock; a reader dropped meanwhile is released afterwards.
func (r *Reader) locked(op func() error) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed.Load() {
		return xmlapi.ErrReaderClosed
	}
	defer func() {
		if r.closed.Load() {
			r.release()
		}
	}()
	return op()
}

// run executes op with the decoder bound to ctx.
func (r *Reader) run(ctx context.Context, op func() error) error {
	return r.locked(func() error {
		if err := r.dec.SetContext(ctx); err != nil {
			return err
		}
		return op()
	})
}

// Fill reads ahead the next events.
func (r *Reader) Fill(ctx context.Context) error {
	return r.run(ctx, r.cursor.Fill)
}

// Node materializes the current element into the node batch.
func (r *Reader) Node(ctx context.Context) error {
	return r.run(ctx, func() error {
		r.node.Reset()
		return r.cursor.Node(&r.node)
	})
}

// Skip discards the current element.
func (r *Reader) Skip(ctx context.Context) error {
	return r.run(ctx, r.cursor.Skip)
}

// Copy writes the current element to w, flushing w to its destination as its buffer fills.
func (r *Reader) Copy(ctx context.Context, w *Writer) error {
	_, err := r.copy(ctx, w, w.drain(ctx))
	return err
}

// CopyBuffered writes the current element to w without touching w's destination.
// Callers use it when the element is buffered, so neither side performs I/O; full
// reports that a stream writer's buffer reached the flush threshold.
func (r *Reader) CopyBuffered(ctx context.Context, w *Writer) (full bool, err error) {
	return r.copy(ctx, w, nil)
}

func (r *Reader) copy(ctx context.Context, w *Writer, drain func(*xml.Encoder) error) (full bool, err error) {
	err = r.run(ctx, func() error {
		return w.run(ctx, func() error {
			if err := r.cursor.CopyTo(w.enc, drain); err != nil {
				return err
			}
			full = w.dst != nil && len(w.enc.Buffered()) >= FlushThreshold
			return nil
		})
	})
	return full, err
}

// Select hands the reader to record selection over paths.
func (r *Reader) Select(paths []xml.Path) error {
	return r.locked(func() error {
		s, err := xml.NewSelector(r.dec, paths)
		if err != nil {
			return err
		}
		r.selector = s
		return nil
	})
}

// ReadRecords reads up to n records into the record batch.
func (r *Reader) ReadRecords(ctx context.Context, n int) (int, error) {
	count := 0
	err := r.run(ctx, func() error {
		if r.selector == nil {
			return xmlapi.ErrNotSelected
		}
		r.records.Reset()
		var err error
		count, err = r.selector.Read(&r.records, n)
		return err
	})
	return count, err
}

// Close releases the reader. It is idempotent and leaves the source open.
func (r *Reader) Close() {
	r.closed.Store(true)
	if r.mu.TryLock() {
		r.release()
		r.mu.Unlock()
	}
}

// Drop implements resource.Dropper.
func (r *Reader) Drop() { r.Close() }

func (r *Reader) release() {
	if r.selector != nil {
		r.selector.Close()
	} else {
		r.dec.Close()
	}
	r.node = xml.Batch{}
	r.records = xml.Batch{}
}

// InsertReader adds r to the table and returns its handle.
func InsertReader(table *resource.Table, r *Reader) uint64 {
	return uint64(table.Insert(TypeReader, r))
}

// GetReader returns the reader stored under id.
func GetReader(table *resource.Table, id uint64) (*Reader, error) {
	v, ok := table.GetTyped(resource.Handle(id), TypeReader)
	if !ok {
		return nil, xmlapi.ErrNotFound
	}
	return v.(*Reader), nil
}
