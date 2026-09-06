// SPDX-License-Identifier: MPL-2.0

package tty

import (
	"context"
	"encoding/base64"
	"strings"
	"sync"
	"time"

	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/security"
	ttyapi "github.com/wippyai/runtime/api/tty"
)

const maxMounts = 128
const mountLease = 30 * time.Second

// A reference is only a lookup key. Authority remains in this owner-side
// record, bound to the exact recipient process and authenticated peer.
type mountRecord struct {
	done      chan struct{}
	issuer    *viewport
	view      *viewport
	service   *Service
	timer     *time.Timer
	ack       chan struct{}
	recipient pid.PID
	ref       string
	lastReply wireFrame
	lastSeq   uint64
	once      sync.Once
	mu        sync.Mutex
	rights    ttyapi.MountRights
	attached  bool
}

func samePID(a, b pid.PID) bool { return a.Node == b.Node && a.Host == b.Host && a.UniqID == b.UniqID }
func hasRight(r ttyapi.MountRights, right string) bool {
	switch right {
	case ttyapi.RightObserve:
		return r.Observe
	case ttyapi.RightInput:
		return r.Input
	case ttyapi.RightResize:
		return r.Resize
	}
	return false
}
func isMountRef(ref string) bool { return strings.HasPrefix(ref, "ttym1.") }
func mountNode(ref string) (string, error) {
	parts := strings.Split(ref, ".")
	if len(parts) != 3 || parts[0] != "ttym1" || len(parts[2]) != 32 {
		return "", ttyapi.ErrInvalidGrant
	}
	b, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || len(b) == 0 || len(b) > 255 {
		return "", ttyapi.ErrInvalidGrant
	}
	if _, err := base64.RawURLEncoding.DecodeString(parts[2]); err != nil {
		return "", ttyapi.ErrInvalidGrant
	}
	return string(b), nil
}

func (v *viewport) Mount(ctx context.Context, recipient pid.PID, rights ttyapi.MountRights) (string, error) {
	owner, ok := runtime.GetFramePID(ctx)
	if !ok || !samePID(owner, v.owner) || !samePID(owner, v.session.creator) || v.producerGrant == "" ||
		!security.IsAllowed(ctx, "tty.mount", v.Handle(), nil) {
		return "", ttyapi.ErrPermissionDenied
	}
	if recipient.Node == "" || recipient.Host == "" || recipient.UniqID == "" || (!rights.Observe && !rights.Input && !rights.Resize) {
		return "", ttyapi.ErrInvalidGrant
	}
	for _, right := range []string{ttyapi.RightObserve, ttyapi.RightInput, ttyapi.RightResize} {
		if hasRight(rights, right) && (!hasRight(v.rights, right) || !security.IsAllowed(ctx, right, v.Handle(), nil)) {
			return "", ttyapi.ErrPermissionDenied
		}
	}
	key, err := token("")
	if err != nil {
		return "", err
	}
	ref := "ttym1." + base64.RawURLEncoding.EncodeToString([]byte(v.owner.Node)) + "." + key
	s := v.session.service
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || v.closed.Load() {
		return "", ttyapi.ErrViewportClosed
	}
	if len(s.mounts) >= maxMounts {
		return "", ttyapi.ErrMeshBusy
	}
	ss := v.session
	ss.mu.Lock()
	defer ss.mu.Unlock()
	if ss.closed {
		return "", ttyapi.ErrViewportClosed
	}
	ss.viewers[recipient]++
	child := ss.newViewportLocked(recipient, "")
	child.rights = rights
	m := &mountRecord{service: s, issuer: v, view: child, recipient: recipient, rights: rights, ref: ref, done: make(chan struct{}), ack: make(chan struct{}, 1)}
	// Timer setup is serialized with close by the service lock: the callback
	// removes the record before closing it, and cannot run until insertion ends.
	m.timer = time.AfterFunc(mountLease, func() { s.removeMount(ref) })
	s.mounts[ref] = m
	return ref, nil
}

func (v *viewport) Revoke(ctx context.Context, ref string) error {
	owner, ok := runtime.GetFramePID(ctx)
	if !ok || !samePID(owner, v.owner) {
		return ttyapi.ErrPermissionDenied
	}
	s := v.session.service
	s.mu.Lock()
	m := s.mounts[ref]
	if m == nil || m.issuer != v {
		s.mu.Unlock()
		return ttyapi.ErrInvalidGrant
	}
	delete(s.mounts, ref)
	s.mu.Unlock()
	m.close()
	return nil
}
func (s *Service) removeMount(ref string) {
	s.mu.Lock()
	m := s.mounts[ref]
	delete(s.mounts, ref)
	s.mu.Unlock()
	if m != nil {
		m.close()
	}
}
func (m *mountRecord) close() {
	m.once.Do(func() {
		m.mu.Lock()
		m.timer.Stop()
		close(m.done)
		m.mu.Unlock()
		_ = m.view.Close()
	})
}
func (s *Service) revokeIssuer(v *viewport) {
	s.mu.Lock()
	var revoked []*mountRecord
	for ref, m := range s.mounts {
		if m.issuer == v {
			revoked = append(revoked, m)
			delete(s.mounts, ref)
		}
	}
	s.mu.Unlock()
	for _, m := range revoked {
		m.close()
	}
}
func (s *Service) closeOwnerMounts(owner pid.PID) {
	s.mu.Lock()
	var revoked []*mountRecord
	for ref, m := range s.mounts {
		if samePID(owner, m.issuer.owner) || samePID(owner, m.recipient) {
			revoked = append(revoked, m)
			delete(s.mounts, ref)
		}
	}
	mesh := s.mesh
	s.mu.Unlock()
	for _, m := range revoked {
		m.close()
	}
	if mesh != nil {
		mesh.closeOwner(owner)
	}
}
func (s *Service) IsRemote(ref string) bool {
	node, err := mountNode(ref)
	if err != nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.mesh != nil && node != s.mesh.local
}
func (s *Service) attachMount(ctx context.Context, ref string) (ttyapi.Viewport, error) {
	node, err := mountNode(ref)
	if err != nil {
		return nil, err
	}
	owner, ok := runtime.GetFramePID(ctx)
	if !ok {
		return nil, ttyapi.ErrPermissionDenied
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, ttyapi.ErrServiceUnavailable
	}
	mesh := s.mesh
	if mesh != nil && node != mesh.local {
		s.mu.Unlock()
		return mesh.attach(ctx, ref, owner)
	}
	m := s.mounts[ref]
	s.mu.Unlock()
	if m == nil || !samePID(owner, m.recipient) {
		return nil, ttyapi.ErrPermissionDenied
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	select {
	case <-m.done:
		return nil, ttyapi.ErrMountExpired
	default:
	}
	if m.attached {
		return nil, ttyapi.ErrInvalidGrant
	}
	m.attached = true
	// Local references are frame-owned. A remote mount instead renews a lease.
	m.timer.Stop()
	return &mountedViewport{record: m}, nil
}

type mountedViewport struct{ record *mountRecord }

func (v *mountedViewport) Grant() string  { return "" }
func (v *mountedViewport) Handle() string { return v.record.ref }
func (v *mountedViewport) Check(ctx context.Context, right string) error {
	owner, ok := runtime.GetFramePID(ctx)
	if !ok || !samePID(owner, v.record.recipient) || (right != "" && !hasRight(v.record.rights, right)) {
		return ttyapi.ErrPermissionDenied
	}
	if right == "" {
		return nil
	}
	select {
	case <-v.record.done:
		return ttyapi.ErrMountExpired
	default:
		return nil
	}
}
func (v *mountedViewport) Snapshot() ttyapi.Snapshot {
	select {
	case <-v.record.done:
		return ttyapi.Snapshot{}
	default:
		return v.record.view.Snapshot()
	}
}
func (v *mountedViewport) Updates() <-chan ttyapi.Update { return v.record.view.Updates() }
func (v *mountedViewport) Send(e ttyapi.Event) error     { return v.record.view.Send(e) }
func (v *mountedViewport) Resize(w, h int) error         { return v.record.view.Resize(w, h) }
func (v *mountedViewport) Close() error                  { v.record.service.removeMount(v.record.ref); return nil }
