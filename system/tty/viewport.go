// SPDX-License-Identifier: MPL-2.0

package tty

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	ttyapi "github.com/wippyai/runtime/api/tty"
)

type viewport struct {
	updates       <-chan ttyapi.Update
	session       *session
	owner         pid.PID
	producerGrant string
	watchID       uint64
	once          sync.Once
	closed        atomic.Bool
	rights        ttyapi.MountRights
}

func (s *session) newViewport(owner pid.PID, grant string) *viewport {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.newViewportLocked(owner, grant)
}

func (s *session) newViewportLocked(owner pid.PID, grant string) *viewport {
	s.nextWatch++
	ch := make(chan ttyapi.Update, 1)
	s.watches[s.nextWatch] = watch{owner: owner, ch: ch}
	return &viewport{rights: ttyapi.MountRights{Observe: true, Input: true, Resize: true}, session: s, owner: owner, producerGrant: grant, watchID: s.nextWatch, updates: ch}
}

func (v *viewport) Grant() string                 { return v.producerGrant }
func (v *viewport) Handle() string                { return v.session.handle }
func (v *viewport) Updates() <-chan ttyapi.Update { return v.updates }

func (v *viewport) Snapshot() ttyapi.Snapshot {
	if v.closed.Load() || !v.rights.Observe {
		return ttyapi.Snapshot{}
	}
	v.session.mu.RLock()
	defer v.session.mu.RUnlock()
	var cursor *ttyapi.Cursor
	if v.session.cursor != nil {
		copy := *v.session.cursor
		cursor = &copy
	}
	return ttyapi.Snapshot{Revision: v.session.revision, Width: v.session.width,
		Height: v.session.height, Rows: v.session.rows, Cursor: cursor}
}

func (v *viewport) Send(event ttyapi.Event) error {
	if !v.rights.Input {
		return ttyapi.ErrPermissionDenied
	}
	if event.Type == "resize" {
		return v.Resize(event.Width, event.Height)
	}
	if v.closed.Load() {
		return ttyapi.ErrViewportClosed
	}
	v.session.mu.RLock()
	if v.session.closed || !v.session.producer || v.session.router == nil {
		v.session.mu.RUnlock()
		return ttyapi.ErrViewportClosed
	}
	if !v.session.inputOpen {
		v.session.mu.RUnlock()
		return ttyapi.ErrInputInactive
	}
	target, router := v.session.target, v.session.router
	v.session.mu.RUnlock()
	return sendEvent(router, target, event)
}

func (v *viewport) Resize(width, height int) error {
	if !v.rights.Resize {
		return ttyapi.ErrPermissionDenied
	}
	if err := ttyapi.ValidateViewportSize(width, height); err != nil {
		return err
	}
	if v.closed.Load() {
		return ttyapi.ErrViewportClosed
	}
	v.session.mu.Lock()
	if v.session.closed {
		v.session.mu.Unlock()
		return ttyapi.ErrViewportClosed
	}
	changed := v.session.width != width || v.session.height != height
	v.session.width, v.session.height = width, height
	if changed {
		v.session.revision++
		update := ttyapi.Update{Revision: v.session.revision}
		for _, watcher := range v.session.watches {
			publishLatest(watcher.ch, update)
		}
	}
	target, router := v.session.target, v.session.router
	connected := v.session.producer && router != nil
	v.session.mu.Unlock()
	if !connected {
		return nil
	}
	return sendEvent(router, target, ttyapi.Event{Type: "resize", Width: width, Height: height})
}

func (v *viewport) Close() error {
	v.once.Do(func() {
		v.closed.Store(true)
		v.session.service.revokeIssuer(v)
		v.session.mu.Lock()
		if watcher, ok := v.session.watches[v.watchID]; ok {
			close(watcher.ch)
			delete(v.session.watches, v.watchID)
		}
		if count := v.session.viewers[v.owner]; count > 1 {
			v.session.viewers[v.owner] = count - 1
		} else {
			delete(v.session.viewers, v.owner)
		}
		v.session.mu.Unlock()
		v.session.service.collect(v.session)
	})
	return nil
}

func sendEvent(router relay.Receiver, target pid.PID, event ttyapi.Event) error {
	pkg := relay.AcquirePackage()
	pkg.Target = target
	pkg.AddMessage(ttyapi.TopicEvents, payload.New(&event))
	if err := router.Send(pkg); err != nil {
		relay.ReleasePackage(pkg)
		return err
	}
	return nil
}

func (v *viewport) Check(ctx context.Context, right string) error {
	owner, ok := runtime.GetFramePID(ctx)
	if !ok || !samePID(owner, v.owner) || (right != "" && !hasRight(v.rights, right)) {
		return ttyapi.ErrPermissionDenied
	}
	if right != "" && v.closed.Load() {
		return ttyapi.ErrViewportClosed
	}
	return nil
}
