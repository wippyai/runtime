// SPDX-License-Identifier: MPL-2.0

package tty

import (
	"sync"
	"sync/atomic"

	"github.com/wippyai/runtime/api/pid"
	ttyapi "github.com/wippyai/runtime/api/tty"
)

type port struct {
	session   *session
	input     *input
	surface   *surface
	once      sync.Once
	surfaceMu sync.Mutex
	closed    bool
}

func (p *port) InputController() ttyapi.InputController { return p.input }
func (p *port) OpenSurface(ttyapi.SurfaceOptions) (ttyapi.Surface, error) {
	p.surfaceMu.Lock()
	defer p.surfaceMu.Unlock()
	if p.closed {
		return nil, ttyapi.ErrInvalidPort
	}
	if p.surface != nil {
		return nil, ttyapi.ErrSurfaceOpen
	}
	s := &surface{session: p.session, owner: p}
	p.surface = s
	return s, nil
}
func (p *port) Close() error {
	p.once.Do(func() {
		p.surfaceMu.Lock()
		p.closed = true
		surface := p.surface
		p.surfaceMu.Unlock()
		if surface != nil {
			_ = surface.Close()
		}
		ss := p.session
		ss.mu.Lock()
		ss.producer = false
		ss.target, ss.router = pid.PID{}, nil
		ss.mu.Unlock()
		ss.service.collect(ss)
	})
	return nil
}

type surface struct {
	session *session
	owner   *port
	once    sync.Once
	closed  atomic.Bool
}

func (s *surface) Present(frame ttyapi.Frame) (ttyapi.PresentStats, error) {
	ss := s.session
	ss.mu.Lock()
	if s.closed.Load() || ss.closed || !ss.producer {
		ss.mu.Unlock()
		return ttyapi.PresentStats{}, ttyapi.ErrViewportClosed
	}
	changed := changedRows(ss.rows, frame.Rows)
	forced := ss.invalid
	if forced && changed == 0 {
		changed = len(frame.Rows)
	}
	// A nil cursor means row-only presentation and preserves terminal state,
	// matching the Frame contract and physical surface implementation.
	cursorChanged := frame.Cursor != nil && !sameCursor(ss.cursor, frame.Cursor)
	if forced || changed != 0 || cursorChanged {
		ss.rows = append([]string(nil), frame.Rows...)
		ss.invalid = false
		if frame.Cursor != nil {
			copy := *frame.Cursor
			ss.cursor = &copy
		}
		ss.revision++
		update := ttyapi.Update{Revision: ss.revision}
		for _, watcher := range ss.watches {
			select {
			case watcher.ch <- update:
			default:
			}
		}
	}
	ss.mu.Unlock()
	return ttyapi.PresentStats{Rows: len(frame.Rows), ChangedRows: changed}, nil
}

func sameCursor(a, b *ttyapi.Cursor) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func (s *surface) Invalidate() {
	s.session.mu.Lock()
	s.session.invalid = true
	s.session.mu.Unlock()
}

func (s *surface) Close() error {
	s.once.Do(func() {
		s.closed.Store(true)
		s.owner.surfaceMu.Lock()
		if s.owner.surface == s {
			s.owner.surface = nil
		}
		s.owner.surfaceMu.Unlock()
	})
	return nil
}

func changedRows(a, b []string) int {
	limit, changed := len(a), 0
	if len(b) > limit {
		limit = len(b)
	}
	for i := 0; i < limit; i++ {
		if i >= len(a) || i >= len(b) || a[i] != b[i] {
			changed++
		}
	}
	return changed
}

var _ ttyapi.Surface = (*surface)(nil)

type input struct {
	session *session
	started atomic.Bool
}

func (i *input) Start() error { i.started.Store(true); return nil }
func (i *input) Stop() error  { i.started.Store(false); return nil }
func (i *input) ScreenSize() (int, int, error) {
	i.session.mu.RLock()
	defer i.session.mu.RUnlock()
	return i.session.width, i.session.height, nil
}
func (i *input) EnableMouse()  {}
func (i *input) DisableMouse() {}
