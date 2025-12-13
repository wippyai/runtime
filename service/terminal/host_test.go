package terminal

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/service/terminal"
	"github.com/wippyai/runtime/internal/uniqid"
	"github.com/wippyai/runtime/system/logs"
	"github.com/wippyai/runtime/system/scheduler/actor"
	"go.uber.org/zap"
)

func newTestPIDGen() *uniqid.PIDGenerator {
	return uniqid.NewPIDGenerator(uniqid.NewGenerator(), "test-node")
}

func TestNewHost(t *testing.T) {
	id := registry.ID{NS: "test", Name: "host1"}
	cfg := &terminal.HostConfig{}
	factory := &mockFactory{}
	logCtrl := logs.NewConfigurator(nil, zap.NewNop())
	log := zap.NewNop()

	h := NewHost(id, cfg, nil, factory, logCtrl, log)

	assert.NotNil(t, h)
	assert.Equal(t, id, h.id)
	assert.Equal(t, cfg, h.cfg)
	assert.NotNil(t, h.statusCh)
	assert.NotNil(t, h.doneCh)
}

func TestHost_Done(t *testing.T) {
	id := registry.ID{NS: "test", Name: "host1"}
	cfg := &terminal.HostConfig{}
	factory := &mockFactory{}
	logCtrl := logs.NewConfigurator(nil, zap.NewNop())
	log := zap.NewNop()

	h := NewHost(id, cfg, nil, factory, logCtrl, log)

	done := h.Done()
	assert.NotNil(t, done)

	select {
	case <-done:
		t.Fatal("done channel should not be closed initially")
	default:
	}
}

func TestHost_StartStop(t *testing.T) {
	id := registry.ID{NS: "test", Name: "host1"}
	cfg := &terminal.HostConfig{}
	factory := &mockFactory{}
	logCtrl := logs.NewConfigurator(nil, zap.NewNop())
	log := zap.NewNop()

	scheduler := actor.NewScheduler(&mockCommandRegistry{},
		actor.WithWorkers(1),
	)

	h := NewHost(id, cfg, scheduler, factory, logCtrl, log)

	statusCh, err := h.Start(context.Background())
	require.NoError(t, err)
	assert.NotNil(t, statusCh)
	assert.True(t, h.running.Load())

	err = h.Stop(context.Background())
	require.NoError(t, err)
	assert.False(t, h.running.Load())
}

func TestHost_StartTwice(t *testing.T) {
	id := registry.ID{NS: "test", Name: "host1"}
	cfg := &terminal.HostConfig{}
	factory := &mockFactory{}
	logCtrl := logs.NewConfigurator(nil, zap.NewNop())
	log := zap.NewNop()

	scheduler := actor.NewScheduler(&mockCommandRegistry{},
		actor.WithWorkers(1),
	)

	h := NewHost(id, cfg, scheduler, factory, logCtrl, log)

	_, err := h.Start(context.Background())
	require.NoError(t, err)

	_, err = h.Start(context.Background())
	require.Error(t, err)

	_ = h.Stop(context.Background())
}

func TestHost_StopNotRunning(t *testing.T) {
	id := registry.ID{NS: "test", Name: "host1"}
	cfg := &terminal.HostConfig{}
	factory := &mockFactory{}
	logCtrl := logs.NewConfigurator(nil, zap.NewNop())
	log := zap.NewNop()

	h := NewHost(id, cfg, nil, factory, logCtrl, log)

	err := h.Stop(context.Background())
	require.NoError(t, err)
}

func TestHost_Run_NotRunning(t *testing.T) {
	id := registry.ID{NS: "test", Name: "host1"}
	cfg := &terminal.HostConfig{}
	factory := &mockFactory{}
	logCtrl := logs.NewConfigurator(nil, zap.NewNop())
	log := zap.NewNop()

	h := NewHost(id, cfg, nil, factory, logCtrl, log)

	_, err := h.Run(context.Background(), &process.Start{
		Source: registry.ID{NS: "test", Name: "process"},
	})
	require.Error(t, err)
	assert.Equal(t, terminal.ErrHostNotRunning, err)
}

func TestHost_Run_ShuttingDown(t *testing.T) {
	id := registry.ID{NS: "test", Name: "host1"}
	cfg := &terminal.HostConfig{}
	factory := &mockFactory{}
	logCtrl := logs.NewConfigurator(nil, zap.NewNop())
	log := zap.NewNop()

	scheduler := actor.NewScheduler(&mockCommandRegistry{},
		actor.WithWorkers(1),
	)

	h := NewHost(id, cfg, scheduler, factory, logCtrl, log)
	_, _ = h.Start(context.Background())
	h.shutdown.Store(true)

	_, err := h.Run(context.Background(), &process.Start{
		Source: registry.ID{NS: "test", Name: "process"},
	})
	require.Error(t, err)

	h.shutdown.Store(false)
	_ = h.Stop(context.Background())
}

func TestHost_Send_ShuttingDown(t *testing.T) {
	id := registry.ID{NS: "test", Name: "host1"}
	cfg := &terminal.HostConfig{}
	factory := &mockFactory{}
	logCtrl := logs.NewConfigurator(nil, zap.NewNop())
	log := zap.NewNop()

	h := NewHost(id, cfg, nil, factory, logCtrl, log)
	h.shutdown.Store(true)

	err := h.Send(&relay.Package{})
	require.Error(t, err)
}

func TestHost_Terminate(t *testing.T) {
	id := registry.ID{NS: "test", Name: "host1"}
	cfg := &terminal.HostConfig{}
	factory := &mockFactory{}
	logCtrl := logs.NewConfigurator(nil, zap.NewNop())
	log := zap.NewNop()

	h := NewHost(id, cfg, nil, factory, logCtrl, log)

	err := h.Terminate(context.Background(), pid.PID{})
	require.NoError(t, err)
}

func TestHost_PreparePID_WithOptions(t *testing.T) {
	id := registry.ID{NS: "test", Name: "host1"}
	cfg := &terminal.HostConfig{}
	factory := &mockFactory{}
	logCtrl := logs.NewConfigurator(nil, zap.NewNop())
	log := zap.NewNop()

	h := NewHost(id, cfg, nil, factory, logCtrl, log)

	expectedPID := pid.PID{Node: "test-node", Host: "test-host", UniqID: "abc123"}
	opts := attrs.NewBag()
	opts.Set(process.OptionPID, expectedPID)

	start := &process.Start{
		Options: opts,
	}

	appCtx := ctxapi.NewAppContext()
	ctx := ctxapi.WithAppContext(context.Background(), appCtx)
	ctx = process.WithPIDGenerator(ctx, newTestPIDGen())
	resultPID := h.preparePID(ctx, start)
	assert.Equal(t, expectedPID, resultPID)
}

func TestHost_PreparePID_Generated(t *testing.T) {
	id := registry.ID{NS: "test", Name: "host1"}
	cfg := &terminal.HostConfig{}
	factory := &mockFactory{}
	logCtrl := logs.NewConfigurator(nil, zap.NewNop())
	log := zap.NewNop()

	h := NewHost(id, cfg, nil, factory, logCtrl, log)

	start := &process.Start{}

	appCtx := ctxapi.NewAppContext()
	ctx := ctxapi.WithAppContext(context.Background(), appCtx)
	ctx = process.WithPIDGenerator(ctx, newTestPIDGen())
	resultPID := h.preparePID(ctx, start)
	assert.NotEqual(t, pid.PID{}, resultPID)
	assert.Equal(t, pid.NodeID("test-node"), resultPID.Node)
}

func TestHost_PrepareContext(t *testing.T) {
	id := registry.ID{NS: "test", Name: "host1"}
	cfg := &terminal.HostConfig{}
	factory := &mockFactory{}
	logCtrl := logs.NewConfigurator(nil, zap.NewNop())
	log := zap.NewNop()

	h := NewHost(id, cfg, nil, factory, logCtrl, log)
	h.ctx = context.Background()

	processID := pid.PID{Node: "test", Host: "host", UniqID: "test1"}
	start := &process.Start{
		Source: registry.ID{NS: "test", Name: "process"},
		Input:  []payload.Payload{payload.New("arg1"), payload.New("arg2")},
	}

	ctx := ctxapi.NewRootContext()
	frameCtx := h.prepareContext(ctx, processID, start)

	fc := ctxapi.FrameFromContext(frameCtx)
	require.NotNil(t, fc)

	val, ok := fc.Get(runtime.FramePIDKey)
	assert.True(t, ok)
	assert.Equal(t, processID, val)

	val, ok = fc.Get(runtime.FrameIDKey)
	assert.True(t, ok)
	assert.Equal(t, start.Source, val)

	val, ok = fc.Get(terminal.TerminalKey())
	assert.True(t, ok)
	tc, ok := val.(*terminal.PipeContext)
	assert.True(t, ok)
	assert.Equal(t, []string{"arg1", "arg2"}, tc.Args)
}

func TestHost_OnStart(t *testing.T) {
	id := registry.ID{NS: "test", Name: "host1"}
	cfg := &terminal.HostConfig{}
	factory := &mockFactory{}
	logCtrl := logs.NewConfigurator(nil, zap.NewNop())
	log := zap.NewNop()

	h := NewHost(id, cfg, nil, factory, logCtrl, log)

	assert.NotPanics(t, func() {
		h.OnStart(context.Background(), pid.PID{}, nil)
	})
}

func TestHost_OnComplete(t *testing.T) {
	id := registry.ID{NS: "test", Name: "host1"}
	cfg := &terminal.HostConfig{}
	factory := &mockFactory{}
	logCtrl := logs.NewConfigurator(nil, zap.NewNop())
	log := zap.NewNop()

	h := NewHost(id, cfg, nil, factory, logCtrl, log)
	h.ctx = context.Background()

	ctx := ctxapi.NewRootContext()
	ctx, _ = ctxapi.OpenFrameContext(ctx)

	go func() {
		time.Sleep(10 * time.Millisecond)
		h.OnComplete(ctx, pid.PID{}, nil)
	}()

	select {
	case <-h.Done():
	case <-time.After(time.Second):
		t.Fatal("done channel not closed")
	}
}

func TestHost_ImplementsInterfaces(_ *testing.T) {
	var _ process.Host = (*Host)(nil)
	var _ relay.Receiver = (*Host)(nil)
}
