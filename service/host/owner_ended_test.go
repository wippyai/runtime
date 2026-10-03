// SPDX-License-Identifier: MPL-2.0

package host

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	apiruntime "github.com/wippyai/runtime/api/runtime"
	hostapi "github.com/wippyai/runtime/api/service/host"
	sysprocess "github.com/wippyai/runtime/system/process"
	"github.com/wippyai/runtime/system/scheduler/actor"
	"go.uber.org/zap"
)

type ownerEndedLifecycle struct {
	host    *Host
	results chan *apiruntime.Result
}

func (*ownerEndedLifecycle) OnStart(context.Context, pid.PID, process.Process) error { return nil }
func (l *ownerEndedLifecycle) OnComplete(ctx context.Context, p pid.PID, result *apiruntime.Result) {
	l.host.OnComplete(ctx, p, result)
	l.results <- result
}

func TestOwnerCompletionReportsCauseToOwnedDescendants(t *testing.T) {
	lifecycle := &ownerEndedLifecycle{results: make(chan *apiruntime.Result, 2)}
	sched := actor.NewScheduler(nil, actor.WithLifecycle(lifecycle))
	h := NewHost(registry.NewID("test", "host"), &hostapi.EntryConfig{}, sched, &mockFactory{}, newTestPIDGen(), zap.NewNop())
	lifecycle.host = h
	node := &leakNode{hosts: map[pid.HostID]relay.Receiver{"test:host": h}}
	manager := sysprocess.NewManager(node, zap.NewNop())
	ctx := process.WithManager(ctxWithAppContext(), manager)
	_, err := h.Start(ctx)
	require.NoError(t, err)
	defer func() { require.NoError(t, h.Stop(context.Background())) }()
	scope := process.NewExecutionScope(ctx, process.ExecutionFunction, manager)
	ctx, frame := ctxapi.OpenFrameContext(ctx)
	require.NoError(t, frame.SetMultiple(process.ExecutionScopePair(scope)))
	defer ctxapi.ReleaseFrameContext(frame)
	admission := func(p process.Process) *process.Admission {
		return &process.Admission{Factory: func() (process.Process, error) { return p, nil }}
	}
	child := &leakProcess{mode: childBlock, init: func(ctx context.Context) error {
		_, err := manager.Start(ctx, &process.Start{HostID: "test:host", Source: registry.NewID("test", "grandchild"), Admission: admission(&leakProcess{mode: childBlock})})
		return err
	}}
	_, err = manager.Start(ctx, &process.Start{HostID: "test:host", Source: registry.NewID("test", "child"), Admission: admission(child), Options: attrs.Bag{process.ProcessOwnedKey: true}})
	require.NoError(t, err)
	scope.Complete()
	for range 2 {
		select {
		case result := <-lifecycle.results:
			require.ErrorIs(t, result.Error, process.ErrOwnerEnded)
		case <-time.After(5 * time.Second):
			t.Fatal("owned descendant did not complete")
		}
	}
}
