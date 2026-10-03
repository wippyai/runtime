// SPDX-License-Identifier: MPL-2.0

package host

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/queue"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/topology"
)

func TestHostAcceptsAdmission(t *testing.T) {
	var _ process.AdmissionHost = (*Host)(nil)
	assert.True(t, newTestHost().host.AcceptsAdmission())
}

// An admitted start runs the process built by its own factory; the host's
// registry factory is not consulted.
func TestHostRunAdmittedProcess(t *testing.T) {
	started := make(chan pid.PID, 1)
	th := newTestHost(func(th *testHost) {
		th.lifecycle.onStartFunc = func(_ context.Context, p pid.PID, _ process.Process) { started <- p }
	})
	th.start(t)
	defer th.stop()

	var created atomic.Int32
	admitted := &mockProcess{}
	p, err := th.host.Run(ctxWithAppContext(), &process.Start{
		HostID: "test:host",
		Source: registry.NewID("eval.program", "abc"),
		Admission: &process.Admission{
			Factory: func() (process.Process, error) {
				created.Add(1)
				return admitted, nil
			},
			Meta: process.Meta{Method: "main"},
		},
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, created.Load())
	require.EqualValues(t, 0, th.factory.called.Load(), "registry factory must not be used for admissions")

	select {
	case got := <-started:
		require.Equal(t, p, got)
	case <-time.After(2 * time.Second):
		t.Fatal("admitted process did not start")
	}
}

// A named admission never routes to an existing process: the admitted
// program would silently not run.
func TestHostRunAdmissionRejectsTakenName(t *testing.T) {
	th := newTestHost()
	th.start(t)
	defer th.stop()

	existing := pid.PID{Host: "test:host", UniqID: "existing"}
	_, err := th.pidReg.Register("worker", existing)
	require.NoError(t, err)

	var created atomic.Int32
	_, err = th.host.Run(ctxWithAppContext(), &process.Start{
		HostID:  "test:host",
		Source:  registry.NewID("eval.program", "abc"),
		Options: namedOptions("worker"),
		Admission: &process.Admission{
			Factory: func() (process.Process, error) {
				created.Add(1)
				return &mockProcess{}, nil
			},
		},
	})
	require.ErrorIs(t, err, topology.ErrNameAlreadyRegistered)
	got, ok := topology.GetExistingPID(err)
	require.True(t, ok)
	require.Equal(t, existing, got)
	require.EqualValues(t, 0, created.Load())
}

// An admitted process starts from a clean frame: the caller's delivery and
// other caller-scoped values are not inherited, while an ordinary spawn
// inherits them.
func TestHostRunAdmissionDoesNotInheritCallerFrame(t *testing.T) {
	started := make(chan context.Context, 2)
	th := newTestHost(func(th *testHost) {
		th.lifecycle.onStartFunc = func(ctx context.Context, _ pid.PID, _ process.Process) { started <- ctx }
	})
	th.start(t)
	defer th.stop()

	callerCtx, fc := ctxapi.OpenFrameContext(ctxWithAppContext())
	require.NoError(t, queue.WithDelivery(callerCtx, &queue.Delivery{}))
	fc.Seal()

	run := func(admission *process.Admission) bool {
		_, err := th.host.Run(callerCtx, &process.Start{
			HostID:    "test:host",
			Source:    registry.NewID("eval.program", "abc"),
			Admission: admission,
		})
		require.NoError(t, err)
		select {
		case ctx := <-started:
			_, ok := queue.GetDelivery(ctx)
			return ok
		case <-time.After(2 * time.Second):
			t.Fatal("process did not start")
			return false
		}
	}

	admitted := run(&process.Admission{Factory: func() (process.Process, error) { return &mockProcess{}, nil }})
	ordinary := run(nil)
	assert.False(t, admitted, "admitted process inherits the caller's delivery")
	assert.True(t, ordinary, "ordinary spawn inherits the caller's delivery")
}
