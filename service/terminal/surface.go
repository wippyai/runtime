// SPDX-License-Identifier: MPL-2.0

package terminal

import (
	"fmt"
	"io"
	"strconv"
	"sync"

	ttyapi "github.com/wippyai/runtime/api/tty"
)

// Surface is the physical ANSI implementation of tty.Surface.
type Surface struct {
	out      io.Writer
	cursor   *ttyapi.Cursor
	rows     []string
	scratch  []byte
	mu       sync.Mutex
	opts     ttyapi.SurfaceOptions
	opened   bool
	closed   bool
	restored bool
}

func NewSurface(out io.Writer, opts ttyapi.SurfaceOptions) *Surface {
	return &Surface{out: out, opts: opts}
}

func (s *Surface) Present(frame ttyapi.Frame) (ttyapi.PresentStats, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ttyapi.PresentStats{}, fmt.Errorf("surface is closed")
	}
	output := s.scratch[:0]
	if s.opts.Synchronized {
		output = append(output, "\x1b[?2026h"...)
	}
	prefix := len(output)
	if !s.opened {
		if s.opts.AlternateScreen {
			output = append(output, "\x1b[?1049h"...)
		}
		if s.opts.HideCursor {
			output = append(output, "\x1b[?25l"...)
		}
	}
	rows := frame.Rows
	changed, limit := 0, len(rows)
	if len(s.rows) > limit {
		limit = len(s.rows)
	}
	for index := 0; index < limit; index++ {
		current, previous := "", ""
		if index < len(rows) {
			current = rows[index]
		}
		if index < len(s.rows) {
			previous = s.rows[index]
		}
		if current == previous && index < len(rows) && index < len(s.rows) {
			continue
		}
		changed++
		output = append(output, '\x1b', '[')
		output = strconv.AppendInt(output, int64(index+1), 10)
		output = append(output, ';', '1', 'H')
		output = append(output, current...)
		output = append(output, "\x1b[0m\x1b[K"...)
	}
	// Painting rows moves the physical terminal cursor even when the logical
	// frame cursor itself did not change. Cursor placement is therefore dirty
	// whenever either cell damage or cursor state changed, and must be the last
	// operation in the frame transaction.
	effectiveCursor := frame.Cursor
	if effectiveCursor == nil {
		effectiveCursor = s.cursor
	}
	cursorChanged := frame.Cursor != nil && !sameSurfaceCursor(s.cursor, frame.Cursor)
	if effectiveCursor != nil && (changed != 0 || cursorChanged) {
		output = append(output, '\x1b', '[')
		output = strconv.AppendInt(output, int64(max(0, effectiveCursor.Row)+1), 10)
		output = append(output, ';')
		output = strconv.AppendInt(output, int64(max(0, effectiveCursor.Column)+1), 10)
		output = append(output, 'H')
		if effectiveCursor.Visible {
			output = append(output, "\x1b[?25h"...)
		} else {
			output = append(output, "\x1b[?25l"...)
		}
	}
	if len(output) == prefix {
		output = output[:0]
	} else if s.opts.Synchronized {
		output = append(output, "\x1b[?2026l"...)
	}
	if len(output) > 0 {
		written, err := s.out.Write(output)
		if err != nil {
			return ttyapi.PresentStats{}, err
		}
		if written != len(output) {
			return ttyapi.PresentStats{}, io.ErrShortWrite
		}
	}
	s.opened = true
	s.scratch = output
	s.rows = append(s.rows[:0], rows...)
	if frame.Cursor != nil {
		copy := *frame.Cursor
		s.cursor = &copy
	}
	return ttyapi.PresentStats{Rows: len(rows), ChangedRows: changed, Bytes: len(output)}, nil
}

func sameSurfaceCursor(a, b *ttyapi.Cursor) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func (s *Surface) Invalidate() { s.mu.Lock(); s.rows = nil; s.mu.Unlock() }

func (s *Surface) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.restored {
		return nil
	}
	s.closed = true
	restore := make([]byte, 0, 32)
	if s.opts.Synchronized {
		restore = append(restore, "\x1b[?2026l"...)
	}
	restore = append(restore, "\x1b[0m"...)
	restore = append(restore, "\x1b[?25h"...)
	if s.opts.AlternateScreen {
		restore = append(restore, "\x1b[?1049l"...)
	}
	written, err := s.out.Write(restore)
	if err != nil {
		return err
	}
	if written != len(restore) {
		return io.ErrShortWrite
	}
	s.restored = true
	return nil
}

var _ ttyapi.Surface = (*Surface)(nil)
