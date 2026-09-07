// SPDX-License-Identifier: MPL-2.0

package tty

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"sync"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/pid"
	processapi "github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/security"
	ttyapi "github.com/wippyai/runtime/api/tty"
	"github.com/wippyai/runtime/service/terminal"
)

// Service is a zero-goroutine, in-memory viewport broker. The AppContext owns
// the service; process frames own bindings and redeemed ports.
type Service struct {
	mounts   map[string]*mountRecord
	mesh     *meshService
	sessions map[string]*session
	grants   map[string]*session
	mu       sync.Mutex
	closed   bool
}

type session struct {
	page         *ttyapi.Page
	pageRenderer *terminal.PageRenderer
	sourceRows   []string
	router       relay.Receiver
	watches      map[uint64]watch
	service      *Service
	cursor       *ttyapi.Cursor
	viewers      map[pid.PID]int
	creator      pid.PID
	target       pid.PID
	grant        string
	handle       string
	rows         []string
	nextWatch    uint64
	revision     uint64
	width        int
	height       int
	bindings     int
	mu           sync.RWMutex
	inputOpen    bool
	producer     bool
	invalid      bool
	closed       bool
}

type watch struct {
	ch    chan ttyapi.Update
	owner pid.PID
}

func NewService() *Service {
	return &Service{mounts: make(map[string]*mountRecord), sessions: make(map[string]*session), grants: make(map[string]*session)}
}

func token(prefix string) (string, error) {
	var raw [24]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return prefix + base64.RawURLEncoding.EncodeToString(raw[:]), nil
}

func (s *Service) Create(ctx context.Context, width, height int) (ttyapi.Viewport, error) {
	if err := ttyapi.ValidateViewportSize(width, height); err != nil {
		return nil, err
	}
	grant, err := token("vpt1_")
	if err != nil {
		return nil, err
	}
	handle, err := token("vph1_")
	if err != nil {
		return nil, err
	}
	owner, ok := runtime.GetFramePID(ctx)
	if !ok {
		return nil, ttyapi.ErrInvalidGrant
	}
	ss := &session{
		service: s, creator: owner, grant: grant, handle: handle, width: width, height: height,
		viewers: map[pid.PID]int{owner: 1}, watches: make(map[uint64]watch),
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, ttyapi.ErrServiceUnavailable
	}
	s.sessions[handle] = ss
	s.grants[grant] = ss
	return ss.newViewport(owner, grant), nil
}

func (s *Service) Attach(ctx context.Context, handle string) (ttyapi.Viewport, error) {
	if isMountRef(handle) {
		return s.attachMount(ctx, handle)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, ttyapi.ErrServiceUnavailable
	}
	ss := s.sessions[handle]
	if ss == nil {
		return nil, ttyapi.ErrInvalidGrant
	}
	owner, ok := runtime.GetFramePID(ctx)
	if !ok {
		return nil, ttyapi.ErrInvalidGrant
	}
	if !samePID(owner, ss.creator) && !security.IsAllowed(ctx, ttyapi.RightObserve, handle, nil) {
		return nil, ttyapi.ErrPermissionDenied
	}
	ss.mu.Lock()
	if ss.closed {
		ss.mu.Unlock()
		return nil, ttyapi.ErrViewportClosed
	}
	ss.viewers[owner]++
	view := ss.newViewportLocked(owner, "")
	view.rights = ttyapi.MountRights{Observe: true, Input: samePID(owner, ss.creator) || security.IsAllowed(ctx, ttyapi.RightInput, handle, nil), Resize: samePID(owner, ss.creator) || security.IsAllowed(ctx, ttyapi.RightResize, handle, nil)}
	ss.mu.Unlock()
	return view, nil
}

func (s *Service) Binding(grant string) (ttyapi.Binding, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, ttyapi.ErrServiceUnavailable
	}
	ss := s.grants[grant]
	if ss == nil {
		return nil, ttyapi.ErrInvalidGrant
	}
	delete(s.grants, grant) // one-shot handoff
	ss.mu.Lock()
	ss.bindings++
	ss.mu.Unlock()
	return &binding{session: ss}, nil
}

func (s *Service) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	sessions := s.sessions
	mounts := s.mounts
	mesh := s.mesh
	s.mounts = nil
	s.sessions = nil
	s.grants = nil
	s.mu.Unlock()
	if mesh != nil {
		mesh.close()
	}
	for _, m := range mounts {
		m.close()
	}
	for _, ss := range sessions {
		ss.closeAll()
	}
	return nil
}

func (s *Service) OnStart(context.Context, pid.PID, processapi.Process) error { return nil }

// OnComplete deterministically detaches views owned by the exiting process.
// The producer and terminal state remain alive for another shell to attach.
func (s *Service) OnComplete(ctx context.Context, owner pid.PID, _ *runtime.Result) {
	s.closeOwnerMounts(owner)
	// The frame owns either the unresolved binding or its resolved port. Frame
	// contexts do not close stored values themselves, so the lifecycle hook is
	// the canonical release barrier for both paths.
	if frame := ctxapi.FrameFromContext(ctx); frame != nil {
		if attachment, ok := frame.Get(ttyapi.PortKey()); ok {
			switch value := attachment.(type) {
			case ttyapi.Binding:
				_ = value.Close()
			case ttyapi.Port:
				_ = value.Close()
			}
		}
	}
	s.mu.Lock()
	sessions := make([]*session, 0, len(s.sessions))
	for _, ss := range s.sessions {
		sessions = append(sessions, ss)
	}
	s.mu.Unlock()
	for _, ss := range sessions {
		ss.detachAll(owner)
		s.collect(ss)
	}
}

func (s *Service) collect(ss *session) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ss.mu.RLock()
	defer ss.mu.RUnlock()
	if len(ss.viewers) != 0 || ss.bindings != 0 || ss.producer {
		return
	}
	if s.sessions[ss.handle] == ss {
		delete(s.sessions, ss.handle)
		delete(s.grants, ss.grant)
	}
}

func (s *session) closeAll() {
	s.mu.Lock()
	s.closed, s.producer = true, false
	for id, watcher := range s.watches {
		close(watcher.ch)
		delete(s.watches, id)
	}
	s.rows, s.router, s.viewers = nil, nil, nil
	s.mu.Unlock()
}

func (s *session) detachAll(owner pid.PID) {
	s.mu.Lock()
	delete(s.viewers, owner)
	for id, watcher := range s.watches {
		if watcher.owner == owner {
			close(watcher.ch)
			delete(s.watches, id)
		}
	}
	s.mu.Unlock()
}

var _ ttyapi.Service = (*Service)(nil)
var _ processapi.Lifecycle = (*Service)(nil)
