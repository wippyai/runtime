// SPDX-License-Identifier: MPL-2.0

package terminal

import (
	"encoding/base64"
	"fmt"
	"io"
	"strconv"
	"sync"
	"unicode/utf8"

	ttyapi "github.com/wippyai/runtime/api/tty"
)

// Surface is the physical ANSI implementation of tty.Surface.
type Surface struct {
	probe    func() ttyapi.SurfaceCapabilities
	graphics graphicsState
	out      io.Writer
	closeErr error
	cursor   *ttyapi.Cursor
	rows     []string
	scratch  []byte
	mu       sync.Mutex
	opts     ttyapi.SurfaceOptions
	opened   bool
	acquired bool
	invalid  bool
	closed   bool
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
	images, err := ttyapi.RetainPlacements(frame.Images)
	if err != nil {
		return ttyapi.PresentStats{}, err
	}
	defer ttyapi.ClosePlacements(images)
	enabled := false
	if len(images) > 0 {
		enabled = s.capabilities().Images == "kitty"
	}
	if !enabled && len(images) > 0 {
		frame.Rows = placeholderRows(frame.Rows, images)
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
	// Clear removed rows first. After a terminal shrink, cursor moves to
	// those old rows clamp to the new bottom row; painting must follow cleanup.
	removed := limit - len(rows)
	for step := 0; step < limit; step++ {
		index := step - removed
		if step < removed {
			index = len(rows) + step
		}
		current, previous := "", ""
		if index < len(rows) {
			current = rows[index]
		}
		if index < len(s.rows) {
			previous = s.rows[index]
		}
		if !s.invalid && removed == 0 && current == previous && index < len(rows) && index < len(s.rows) {
			continue
		}
		changed++
		output = append(output, '\x1b', '[')
		output = strconv.AppendInt(output, int64(index+1), 10)
		output = append(output, ';', '1', 'H')
		// Clear the old extent before painting. EL after a full-width row
		// erases its last cell while the terminal is in delayed autowrap.
		output = append(output, "\x1b[0m\x1b[K"...)
		output = append(output, current...)
		output = append(output, "\x1b[0m"...)
	}
	if s.invalid && limit == 0 {
		output = append(output, "\x1b[H\x1b[0m\x1b[J"...)
	}
	graphicsStart := len(output)
	var desired map[string]hostPlacement
	if len(images) > 0 || len(s.graphics.known) > 0 {
		var err error
		output, desired, err = s.appendGraphics(output, images, enabled)
		if err != nil {
			s.invalid = true
			return ttyapi.PresentStats{}, err
		}
	}
	graphicsChanged := len(output) != graphicsStart
	// Painting rows moves the physical terminal cursor even when the logical
	// frame cursor itself did not change. Cursor placement is therefore dirty
	// whenever either cell damage or cursor state changed, and must be the last
	// operation in the frame transaction.
	effectiveCursor := frame.Cursor
	if effectiveCursor == nil {
		effectiveCursor = s.cursor
	}
	cursorChanged := frame.Cursor != nil && !sameSurfaceCursor(s.cursor, frame.Cursor)
	if effectiveCursor != nil && (s.invalid || changed != 0 || cursorChanged || graphicsChanged) {
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
		// A short or failed write may still have changed terminal modes or cursor
		// state. Record acquisition before the call so Close performs recovery.
		s.acquired = true
		written, err := s.out.Write(output)
		if err != nil {
			s.invalid = true
			return ttyapi.PresentStats{}, err
		}
		if written != len(output) {
			s.invalid = true
			return ttyapi.PresentStats{}, io.ErrShortWrite
		}
	}
	if desired != nil {
		s.commitGraphics(desired)
	}
	s.opened = true
	s.invalid = false
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

func (s *Surface) Invalidate() {
	s.mu.Lock()
	if !s.closed {
		s.invalid = true
	}
	s.mu.Unlock()
}

func (s *Surface) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return s.closeErr
	}
	s.closed = true
	if !s.acquired {
		return nil
	}
	restore := make([]byte, 0, 32)
	for _, id := range s.graphics.known {
		restore = deleteHostImage(restore, id)
	}
	s.graphics = graphicsState{}
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
		s.closeErr = err
		return s.closeErr
	}
	if written != len(restore) {
		s.closeErr = io.ErrShortWrite
		return s.closeErr
	}
	return nil
}

var _ ttyapi.Surface = (*Surface)(nil)

// Clipboard writes one bounded OSC52 request under the same lock as frames.
// The request is never retained in surface history or repeated by Invalidate.
func (s *Surface) Clipboard(text string) error {
	if len(text) > ttyapi.MaxClipboardBytes || !utf8.ValidString(text) {
		return fmt.Errorf("invalid clipboard text: expected UTF-8 within %d bytes", ttyapi.MaxClipboardBytes)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ttyapi.ErrInvalidPort
	}
	request := "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte(text)) + "\x07"
	n, err := io.WriteString(s.out, request)
	if err == nil && n != len(request) {
		return io.ErrShortWrite
	}
	return err
}

func (s *Surface) Capabilities() ttyapi.SurfaceCapabilities {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.capabilities()
}

func (s *Surface) capabilities() ttyapi.SurfaceCapabilities {
	if s.probe != nil {
		return s.probe()
	}
	return ttyapi.SurfaceCapabilities{Images: "none"}
}
