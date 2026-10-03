// SPDX-License-Identifier: MPL-2.0

package process

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	process "github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
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
