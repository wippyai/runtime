// SPDX-License-Identifier: MPL-2.0

package process

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	process "github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/topology"
	"go.uber.org/zap"
)

type admissionTestProcess struct{}

func (admissionTestProcess) Init(context.Context, string, payload.Payloads) error { return nil }
func (admissionTestProcess) Step([]process.Event, *process.StepOutput) error      { return nil }
func (admissionTestProcess) Close()                                               {}

type admissionHost struct {
	mockHost
	accepts bool
}

func (h *admissionHost) AcceptsAdmission() bool { return h.accepts }

func testAdmission() *process.Admission {
	return &process.Admission{
		Factory: func() (process.Process, error) { return admissionTestProcess{}, nil },
		Meta:    process.Meta{Method: "main"},
	}
}

func TestManagerStartRejectsAdmissionForHostWithoutAdmission(t *testing.T) {
	for name, host := range map[string]process.Host{
		"no capability":     &mockHost{},
		"capability denied": &admissionHost{accepts: false},
	} {
		t.Run(name, func(t *testing.T) {
			node := newMockNode()
			_ = node.RegisterHost("host", host)

			_, err := NewManager(node, zap.NewNop()).Start(context.Background(), &process.Start{
				HostID:    "host",
				Source:    registry.NewID("eval.program", "abc"),
				Admission: testAdmission(),
			})
			require.ErrorIs(t, err, ErrAdmissionUnsupported)
		})
	}
}

func TestManagerStartForwardsAdmissionToAcceptingHost(t *testing.T) {
	node := newMockNode()
	host := &admissionHost{accepts: true}
	_ = node.RegisterHost("host", host)

	_, err := NewManager(node, zap.NewNop()).Start(context.Background(), &process.Start{
		HostID:    "host",
		Source:    registry.NewID("eval.program", "abc"),
		Admission: testAdmission(),
	})
	require.NoError(t, err)
	require.True(t, host.runCalled)
}

// ownedHost accepts frame attachments and records the start it ran.
type ownedHost struct {
	start *process.Start
	mockHost
}

func (h *ownedHost) Run(ctx context.Context, start *process.Start) (pid.PID, error) {
	h.start = start
	return h.mockHost.Run(ctx, start)
}

type stubTerminator struct{ terminated []pid.PID }

func (s *stubTerminator) Terminate(_ context.Context, p pid.PID) error {
	s.terminated = append(s.terminated, p)
	return nil
}

func scopedContext(t *testing.T) (context.Context, *process.ExecutionScope, *stubTerminator) {
	t.Helper()
	ctx, fc := ctxapi.OpenFrameContext(context.Background())
	t.Cleanup(func() { ctxapi.ReleaseFrameContext(fc) })
	term := &stubTerminator{}
	scope := process.NewExecutionScope(ctx, process.ExecutionProcess, term)
	require.NoError(t, fc.SetMultiple(process.ExecutionScopePair(scope)))
	return ctx, scope, term
}

func ownedStart() *process.Start {
	options := attrs.NewBag()
	options.Set(process.ProcessOwnedKey, true)
	return &process.Start{HostID: "host", Source: registry.NewID("app", "worker"), Options: options}
}

func TestManagerStartOwnedChildIsTerminatedWithItsOwner(t *testing.T) {
	node := newMockNode()
	host := &ownedHost{mockHost: mockHost{acceptsAttachments: true}}
	_ = node.RegisterHost("host", host)
	ctx, scope, term := scopedContext(t)

	child, err := NewManager(node, zap.NewNop()).Start(ctx, ownedStart())
	require.NoError(t, err)
	require.True(t, hasFrameAttachments(host.start.Context), "the child carries its ownership registration")

	scope.Complete()
	require.Equal(t, []pid.PID{child}, term.terminated)
}

func TestManagerStartOwnedRequiresScope(t *testing.T) {
	node := newMockNode()
	host := &ownedHost{mockHost: mockHost{acceptsAttachments: true}}
	_ = node.RegisterHost("host", host)

	_, err := NewManager(node, zap.NewNop()).Start(context.Background(), ownedStart())
	require.ErrorIs(t, err, process.ErrOwnerRequired)
	require.False(t, host.runCalled)
}

func TestManagerStartOwnedRefusedAfterOwnerEnded(t *testing.T) {
	node := newMockNode()
	host := &ownedHost{mockHost: mockHost{acceptsAttachments: true}}
	_ = node.RegisterHost("host", host)
	ctx, scope, _ := scopedContext(t)
	scope.Complete()

	_, err := NewManager(node, zap.NewNop()).Start(ctx, ownedStart())
	require.ErrorIs(t, err, process.ErrOwnerEnded)
	require.False(t, host.runCalled)
}

func TestManagerStartOwnedRejectedByHostWithoutAttachments(t *testing.T) {
	node := newMockNode()
	host := &ownedHost{}
	_ = node.RegisterHost("host", host)
	ctx, scope, term := scopedContext(t)

	_, err := NewManager(node, zap.NewNop()).Start(ctx, ownedStart())
	require.ErrorIs(t, err, ErrFrameAttachmentsUnsupported, "a host that cannot carry ownership cannot run owned children")
	scope.Complete()
	require.Empty(t, term.terminated, "the reservation was rolled back")
}

func TestManagerStartChildrenOfOwnedProcessAreOwned(t *testing.T) {
	node := newMockNode()
	host := &ownedHost{mockHost: mockHost{acceptsAttachments: true}}
	_ = node.RegisterHost("host", host)

	owner, ownerScope, _ := scopedContext(t)
	pairs, registration, err := ownerScope.Reserve()
	require.NoError(t, err)
	require.NoError(t, registration.Bind(pid.PID{Host: "host", UniqID: "owned"}))

	ownedCtx, fc := ctxapi.OpenFrameContext(owner)
	defer ctxapi.ReleaseFrameContext(fc)
	term := &stubTerminator{}
	ownScope := process.NewExecutionScope(ownedCtx, process.ExecutionProcess, term)
	require.NoError(t, fc.SetMultiple(append(pairs, process.ExecutionScopePair(ownScope))...))

	grandchild, err := NewManager(node, zap.NewNop()).Start(ownedCtx, &process.Start{
		HostID: "host", Source: registry.NewID("app", "worker"),
	})
	require.NoError(t, err, "no owned option is needed")
	ownScope.Complete()
	require.Equal(t, []pid.PID{grandchild}, term.terminated, "an owned process owns what it spawns")
}

// signalingHost answers a named spawn with an existing process, as host
// spawn-or-signal does: the start's attachments are rolled back and no new
// process runs. beforeReturn runs between the rollback and the reply.
type signalingHost struct {
	beforeReturn func()
	existing     pid.PID
	mockHost
}

func (h *signalingHost) Run(_ context.Context, start *process.Start) (pid.PID, error) {
	for _, pair := range start.Context {
		if attachment, ok := pair.Value.(ctxapi.FrameAttachment); ok {
			_ = attachment.Rollback()
		}
	}
	if h.beforeReturn != nil {
		h.beforeReturn()
	}
	return h.existing, nil
}

func TestManagerStartOwnedNeverAdoptsAnExistingProcess(t *testing.T) {
	for _, ownerEnds := range []bool{false, true} {
		node := newMockNode()
		ctx, scope, term := scopedContext(t)
		existing := pid.PID{Host: "host", UniqID: "unrelated"}
		host := &signalingHost{existing: existing, mockHost: mockHost{acceptsAttachments: true}}
		if ownerEnds {
			host.beforeReturn = scope.Complete
		}
		_ = node.RegisterHost("host", host)

		_, err := NewManager(node, zap.NewNop()).Start(ctx, ownedStart())
		require.ErrorIs(t, err, topology.ErrNameAlreadyRegistered, "an owned spawn starts a process or fails")
		got, ok := topology.GetExistingPID(err)
		require.True(t, ok)
		require.Equal(t, existing, got)
		require.False(t, host.terminateCalled, "the existing process belongs to someone else")
		scope.Complete()
		require.Empty(t, term.terminated, "nothing was started for the owner")
	}
}

// stoppingHost hands out unique PIDs and records the processes the manager
// terminates itself.
type stoppingHost struct {
	started    map[pid.PID]struct{}
	terminated map[pid.PID]struct{}
	mockHost
	mu   sync.Mutex
	next int
}

func (h *stoppingHost) Run(_ context.Context, start *process.Start) (pid.PID, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.next++
	p := pid.PID{Host: start.HostID, UniqID: fmt.Sprintf("p%d", h.next)}
	h.started[p] = struct{}{}
	return p, nil
}

func (h *stoppingHost) Terminate(_ context.Context, p pid.PID) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.terminated[p] = struct{}{}
	return nil
}

type syncTerminator struct {
	terminated map[pid.PID]struct{}
	mu         sync.Mutex
}

func (s *syncTerminator) Terminate(_ context.Context, p pid.PID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.terminated[p] = struct{}{}
	return nil
}

// An owner can end while its children start: every process the host started
// is stopped by the scope or by the manager, and no registration remains.
func TestManagerStartOwnedWhileOwnerEnds(t *testing.T) {
	for round := 0; round < 200; round++ {
		node := newMockNode()
		host := &stoppingHost{
			mockHost:   mockHost{acceptsAttachments: true},
			started:    map[pid.PID]struct{}{},
			terminated: map[pid.PID]struct{}{},
		}
		_ = node.RegisterHost("host", host)
		ctx, fc := ctxapi.OpenFrameContext(context.Background())
		term := &syncTerminator{terminated: map[pid.PID]struct{}{}}
		scope := process.NewExecutionScope(ctx, process.ExecutionProcess, term)
		require.NoError(t, fc.SetMultiple(process.ExecutionScopePair(scope)))
		manager := NewManager(node, zap.NewNop())

		var wg sync.WaitGroup
		for i := 0; i < 4; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < 20; j++ {
					_, _ = manager.Start(ctx, ownedStart())
				}
			}()
		}
		scope.Complete()
		wg.Wait()

		host.mu.Lock()
		term.mu.Lock()
		for p := range host.started {
			_, byScope := term.terminated[p]
			_, byManager := host.terminated[p]
			require.True(t, byScope || byManager, "process %s outlives its owner", p)
		}
		term.mu.Unlock()
		host.mu.Unlock()
		require.Zero(t, scope.Owned())
		ctxapi.ReleaseFrameContext(fc)
	}
}
