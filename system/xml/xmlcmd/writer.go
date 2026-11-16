// SPDX-License-Identifier: MPL-2.0

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

// FlushThreshold is the buffered output size at which a stream writer flushes.
const FlushThreshold = 64 << 10

// Writer owns an encoder and its destination. Without a destination the output
// accumulates in the encoder.
type Writer struct {
	enc    *xml.Encoder
	dst    io.Writer
	sync   func() error
	mu     sync.Mutex
	closed atomic.Bool
}

// NewWriter creates a writer. dst may be nil for an in-memory writer; sync, when
// set, flushes the destination itself.
func NewWriter(opts xml.EncodeOptions, dst io.Writer, sync func() error) (*Writer, error) {
	enc, err := xml.NewEncoder(opts)
	if err != nil {
		return nil, err
	}
	return &Writer{enc: enc, dst: dst, sync: sync}, nil
}

// Streaming reports whether the writer has a destination.
func (w *Writer) Streaming() bool { return w.dst != nil }

// Write runs op on the encoder under the writer's lock. full reports that a stream
// writer's buffer reached the flush threshold.
func (w *Writer) Write(op func(*xml.Encoder) error) (full bool, err error) {
	err = w.locked(func() error {
		if err := op(w.enc); err != nil {
			return err
		}
		full = w.dst != nil && len(w.enc.Buffered()) >= FlushThreshold
		return nil
	})
	return full, err
}

// Err returns the terminal error of the writer: the encoder's failure first, then closed.
// Release leaves the encoder's error in place, so it is valid after Close.
func (w *Writer) Err() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.enc.Err(); err != nil {
		return err
	}
	if w.closed.Load() {
		return xmlapi.ErrWriterClosed
	}
	return nil
}

// Finish completes the document and returns it for an in-memory writer.
func (w *Writer) Finish() ([]byte, error) {
	var doc []byte
	err := w.locked(func() error {
		if err := w.enc.Finish(); err != nil {
			return err
		}
		if w.dst == nil {
			doc = w.enc.Drain()
		}
		return nil
	})
	return doc, err
}

// Closed reports whether the writer has been released.
func (w *Writer) Closed() bool { return w.closed.Load() }

// locked runs op holding the writer's lock; a writer dropped meanwhile is released afterwards.
func (w *Writer) locked(op func() error) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.closed.Load() {
		return xmlapi.ErrWriterClosed
	}
	defer func() {
		if w.closed.Load() {
			w.release()
		}
	}()
	return op()
}

func (w *Writer) run(ctx context.Context, op func() error) error {
	return w.locked(func() error {
		if err := ctx.Err(); err != nil {
			return w.enc.Abort(err)
		}
		return op()
	})
}

// drain flushes the buffer once it reaches the threshold.
func (w *Writer) drain(ctx context.Context) func(*xml.Encoder) error {
	if w.dst == nil {
		return nil
	}
	return func(enc *xml.Encoder) error {
		if len(enc.Buffered()) < FlushThreshold {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		return enc.Flush(w.dst)
	}
}

// Flush writes buffered output to the destination; sync also flushes the destination.
func (w *Writer) Flush(ctx context.Context, sync bool) error {
	return w.run(ctx, func() error {
		if w.dst == nil {
			return w.enc.Err()
		}
		if err := w.enc.Flush(w.dst); err != nil {
			return err
		}
		if sync && w.sync != nil {
			if err := w.sync(); err != nil {
				return w.enc.Abort(err)
			}
		}
		return nil
	})
}

// Close releases the writer without completing the document. It is idempotent and
// leaves the destination open.
func (w *Writer) Close() {
	w.closed.Store(true)
	if w.mu.TryLock() {
		w.release()
		w.mu.Unlock()
	}
}

// Drop implements resource.Dropper.
func (w *Writer) Drop() { w.Close() }

func (w *Writer) release() { w.enc.Close() }

// InsertWriter adds w to the table and returns its handle.
func InsertWriter(table *resource.Table, w *Writer) uint64 {
	return uint64(table.Insert(TypeWriter, w))
}

// GetWriter returns the writer stored under id.
func GetWriter(table *resource.Table, id uint64) (*Writer, error) {
	v, ok := table.GetTyped(resource.Handle(id), TypeWriter)
	if !ok {
		return nil, xmlapi.ErrNotFound
	}
	return v.(*Writer), nil
}
