// SPDX-License-Identifier: MPL-2.0

package tty

import (
	"slices"
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
	closed    atomic.Bool
}

func (p *port) InputController() ttyapi.InputController { return p.input }
func (p *port) OpenSurface(ttyapi.SurfaceOptions) (ttyapi.Surface, error) {
	p.surfaceMu.Lock()
	defer p.surfaceMu.Unlock()
	if p.closed.Load() {
		return nil, ttyapi.ErrInvalidPort
	}
	if p.surface != nil {
		return nil, ttyapi.ErrSurfaceOpen
	}
	s := &surface{session: p.session, owner: p}
	p.surface = s
	return s, nil
}

// Close retires the producer and arms a fresh creator grant for its
// replacement. The retired port stays closed, so it cannot present or change
// input once a successor resolves.
func (p *port) Close() error {
	var err error
	p.once.Do(func() {
		p.surfaceMu.Lock()
		p.closed.Store(true)
		surface := p.surface
		p.surfaceMu.Unlock()
		if surface != nil {
			_ = surface.Close()
		}
		var next string
		next, err = token("vpt1_")
		ss, service := p.session, p.session.service
		service.mu.Lock()
		ss.mu.Lock()
		ss.inputOpen, ss.producer = false, false
		ss.target, ss.router = pid.PID{}, nil
		if err == nil && !service.closed && !ss.closed && service.sessions[ss.handle] == ss {
			ss.grant = next
			service.grants[next] = ss
		}
		ss.mu.Unlock()
		service.mu.Unlock()
		service.collect(ss)
	})
	return err
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
	images, err := ttyapi.RetainPlacements(frame.Images)
	if err != nil {
		ss.mu.Unlock()
		return ttyapi.PresentStats{}, err
	}
	placements := ttyapi.PlacementMetadata(images)
	imagesChanged := !slices.Equal(ss.placements, placements)
	changed := changedRows(ss.sourceRows, frame.Rows)
	forced := ss.invalid
	if forced && changed == 0 {
		changed = len(frame.Rows)
	}
	// A nil cursor means row-only presentation and preserves terminal state,
	// matching the Frame contract and physical surface implementation.
	cursorChanged := frame.Cursor != nil && !sameCursor(ss.cursor, frame.Cursor)
	if forced || changed != 0 || cursorChanged || imagesChanged {
		ttyapi.ClosePlacements(ss.images)
		ss.images, ss.placements = images, placements
		images = nil
		if changed != 0 {
			previousSource, previousRows := ss.sourceRows, ss.rows
			ss.sourceRows = append([]string(nil), frame.Rows...)
			ss.resolvePageRows(previousSource, previousRows)
		}
		ss.invalid = false
		if frame.Cursor != nil {
			copy := *frame.Cursor
			ss.cursor = &copy
		}
		ss.revision++
		update := ttyapi.Update{Revision: ss.revision}
		for _, watcher := range ss.watches {
			publishLatest(watcher.ch, update)
		}
	}
	ttyapi.ClosePlacements(images)
	ss.mu.Unlock()
	return ttyapi.PresentStats{Rows: len(frame.Rows), ChangedRows: changed}, nil
}

// publishLatest replaces the single buffered watermark. Callers serialize
// producers with session.mu, so no update can race the replacement.
func publishLatest(ch chan ttyapi.Update, update ttyapi.Update) {
	select {
	case ch <- update:
		return
	default:
	}
	select {
	case <-ch:
	default:
	}
	select {
	case ch <- update:
	default:
	}
}

func sameCursor(a, b *ttyapi.Cursor) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

func (s *surface) Invalidate() {
	s.owner.surfaceMu.Lock()
	active := s.owner.surface == s && !s.closed.Load()
	if !active {
		s.owner.surfaceMu.Unlock()
		return
	}
	s.session.mu.Lock()
	s.session.invalid = true
	s.session.mu.Unlock()
	s.owner.surfaceMu.Unlock()
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
	port *port
}

func (i *input) Start() error {
	ss := i.port.session
	ss.mu.Lock()
	defer ss.mu.Unlock()
	if i.port.closed.Load() || ss.closed || !ss.producer {
		return ttyapi.ErrViewportClosed
	}
	ss.inputOpen = true
	return nil
}
func (i *input) Stop() error {
	ss := i.port.session
	ss.mu.Lock()
	if !i.port.closed.Load() {
		ss.inputOpen = false
	}
	ss.mu.Unlock()
	return nil
}
func (i *input) ScreenSize() (int, int, error) {
	ss := i.port.session
	ss.mu.RLock()
	defer ss.mu.RUnlock()
	if i.port.closed.Load() {
		return 0, 0, ttyapi.ErrViewportClosed
	}
	return ss.width, ss.height, nil
}
func (i *input) EnableMouse()  {}
func (i *input) DisableMouse() {}

func (s *surface) Capabilities() ttyapi.SurfaceCapabilities {
	return ttyapi.SurfaceCapabilities{Images: "native"}
}
