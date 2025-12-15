package host

import (
	"context"
	"errors"
	"testing"

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
	hostapi "github.com/wippyai/runtime/api/service/host"
	"github.com/wippyai/runtime/internal/uniqid"
	"github.com/wippyai/runtime/system/scheduler/actor"
	"go.uber.org/zap"
)

func newTestPIDGen() *uniqid.PIDGenerator {
	return uniqid.NewPIDGenerator(uniqid.NewGenerator(), "test-node")
}

func ctxWithPIDGen() context.Context {
	appCtx := ctxapi.NewAppContext()
	ctx := ctxapi.WithAppContext(context.Background(), appCtx)
	return process.WithPIDGenerator(ctx, newTestPIDGen())
}

type mockProcess struct{}

func (m *mockProcess) Init(_ctx context.Context, _method string, _input payload.Payloads) error {
	return nil
}

func (m *mockProcess) Step(_events []process.Event, out *process.StepOutput) error {
	out.Done(nil)
	return nil
}

func (m *mockProcess) Close() {}

type mockFactory struct {
	proc process.Process
	err  error
}

func (f *mockFactory) Create(_source registry.ID) (process.Process, *process.Meta, error) {
	if f.err != nil {
		return nil, nil, f.err
	}
	return f.proc, nil, nil
}

func TestNewHost(t *testing.T) {
	id := registry.NewID("test", "host")
	cfg := &hostapi.EntryConfig{}
	scheduler := actor.NewScheduler(nil)
	factory := &mockFactory{proc: &mockProcess{}}
	pidGen := newTestPIDGen()
	logger := zap.NewNop()

	h := NewHost(id, cfg, scheduler, factory, pidGen, logger)

	assert.NotNil(t, h)
	assert.Equal(t, id, h.id)
	assert.Equal(t, cfg, h.cfg)
	assert.Equal(t, scheduler, h.scheduler)
}

func TestHost_RunNotRunning(t *testing.T) {
	id := registry.NewID("test", "host")
	cfg := &hostapi.EntryConfig{}
	scheduler := actor.NewScheduler(nil)
	factory := &mockFactory{proc: &mockProcess{}}
	pidGen := newTestPIDGen()
	logger := zap.NewNop()

	h := NewHost(id, cfg, scheduler, factory, pidGen, logger)

	_, err := h.Run(context.Background(), &process.Start{
		Source: registry.NewID("test", "proc"),
	})

	assert.ErrorIs(t, err, ErrHostNotRunning)
}

func TestHost_StartStop(t *testing.T) {
	id := registry.NewID("test", "host")
	cfg := &hostapi.EntryConfig{}
	scheduler := actor.NewScheduler(nil)
	factory := &mockFactory{proc: &mockProcess{}}
	pidGen := newTestPIDGen()
	logger := zap.NewNop()

	h := NewHost(id, cfg, scheduler, factory, pidGen, logger)

	_, err := h.Start(context.Background())
	require.NoError(t, err)

	err = h.Stop(context.Background())
	require.NoError(t, err)
}

func TestHost_StartAlreadyRunning(t *testing.T) {
	id := registry.NewID("test", "host")
	cfg := &hostapi.EntryConfig{}
	scheduler := actor.NewScheduler(nil)
	factory := &mockFactory{proc: &mockProcess{}}
	pidGen := newTestPIDGen()
	logger := zap.NewNop()

	h := NewHost(id, cfg, scheduler, factory, pidGen, logger)

	_, err := h.Start(context.Background())
	require.NoError(t, err)

	_, err = h.Start(context.Background())
	assert.ErrorIs(t, err, ErrHostAlreadyRunning)

	_ = h.Stop(context.Background())
}

func TestHost_StopNotRunning(t *testing.T) {
	id := registry.NewID("test", "host")
	cfg := &hostapi.EntryConfig{}
	scheduler := actor.NewScheduler(nil)
	factory := &mockFactory{proc: &mockProcess{}}
	pidGen := newTestPIDGen()
	logger := zap.NewNop()

	h := NewHost(id, cfg, scheduler, factory, pidGen, logger)

	err := h.Stop(context.Background())
	assert.NoError(t, err)
}

func TestHost_SendShuttingDown(t *testing.T) {
	id := registry.NewID("test", "host")
	cfg := &hostapi.EntryConfig{}
	scheduler := actor.NewScheduler(nil)
	factory := &mockFactory{proc: &mockProcess{}}
	pidGen := newTestPIDGen()
	logger := zap.NewNop()

	h := NewHost(id, cfg, scheduler, factory, pidGen, logger)
	h.shutdown.Store(true)

	err := h.Send(&relay.Package{})
	assert.ErrorIs(t, err, ErrHostShuttingDown)
}

func TestHost_RunShuttingDown(t *testing.T) {
	id := registry.NewID("test", "host")
	cfg := &hostapi.EntryConfig{}
	scheduler := actor.NewScheduler(nil)
	factory := &mockFactory{proc: &mockProcess{}}
	pidGen := newTestPIDGen()
	logger := zap.NewNop()

	h := NewHost(id, cfg, scheduler, factory, pidGen, logger)
	h.running.Store(true)
	h.shutdown.Store(true)

	_, err := h.Run(context.Background(), &process.Start{
		Source: registry.NewID("test", "proc"),
	})

	assert.ErrorIs(t, err, ErrHostShuttingDown)
}

func TestHost_OnStartOnComplete(t *testing.T) {
	id := registry.NewID("test", "host")
	cfg := &hostapi.EntryConfig{}
	scheduler := actor.NewScheduler(nil)
	factory := &mockFactory{proc: &mockProcess{}}
	pidGen := newTestPIDGen()
	logger := zap.NewNop()

	h := NewHost(id, cfg, scheduler, factory, pidGen, logger)

	h.OnStart(context.Background(), pid.PID{}, &mockProcess{})
	h.OnComplete(context.Background(), pid.PID{}, &runtime.Result{})
}

func TestHost_SendSuccess(t *testing.T) {
	id := registry.NewID("test", "host")
	cfg := &hostapi.EntryConfig{}
	scheduler := actor.NewScheduler(nil)
	factory := &mockFactory{proc: &mockProcess{}}
	pidGen := newTestPIDGen()
	logger := zap.NewNop()

	h := NewHost(id, cfg, scheduler, factory, pidGen, logger)

	_, err := h.Start(context.Background())
	require.NoError(t, err)

	// Send should return error from scheduler (process not found) but not shutdown error
	err = h.Send(&relay.Package{Target: pid.PID{}})
	assert.NotErrorIs(t, err, ErrHostShuttingDown)

	_ = h.Stop(context.Background())
}

func TestHost_Terminate(t *testing.T) {
	id := registry.NewID("test", "host")
	cfg := &hostapi.EntryConfig{}
	scheduler := actor.NewScheduler(nil)
	factory := &mockFactory{proc: &mockProcess{}}
	pidGen := newTestPIDGen()
	logger := zap.NewNop()

	h := NewHost(id, cfg, scheduler, factory, pidGen, logger)

	_, err := h.Start(context.Background())
	require.NoError(t, err)

	// Terminate on non-existent process
	err = h.Terminate(context.Background(), pid.PID{})
	assert.Error(t, err)

	_ = h.Stop(context.Background())
}

func TestHost_RunFactoryError(t *testing.T) {
	id := registry.NewID("test", "host")
	cfg := &hostapi.EntryConfig{}
	scheduler := actor.NewScheduler(nil)
	factoryErr := errors.New("factory error")
	factory := &mockFactory{err: factoryErr}
	pidGen := newTestPIDGen()
	logger := zap.NewNop()

	h := NewHost(id, cfg, scheduler, factory, pidGen, logger)
	h.running.Store(true)

	_, err := h.Run(ctxWithPIDGen(), &process.Start{
		Source: registry.NewID("test", "proc"),
	})

	assert.ErrorIs(t, err, factoryErr)
}

func TestHost_PreparePID_ExplicitPID(t *testing.T) {
	id := registry.NewID("test", "host")
	cfg := &hostapi.EntryConfig{}
	scheduler := actor.NewScheduler(nil)
	factory := &mockFactory{proc: &mockProcess{}}
	pidGen := newTestPIDGen()
	logger := zap.NewNop()

	h := NewHost(id, cfg, scheduler, factory, pidGen, logger)

	explicitPID := pid.PID{Node: "test-node", Host: "test:host", UniqID: "explicit-123"}
	opts := attrs.NewBag()
	opts.Set(process.OptionPID, explicitPID)

	resultPID := h.preparePID(ctxWithPIDGen(), &process.Start{
		Source:  registry.NewID("test", "proc"),
		Options: opts,
	})

	assert.Equal(t, explicitPID, resultPID)
}

func TestHost_PreparePID_Generated(t *testing.T) {
	id := registry.NewID("test", "host")
	cfg := &hostapi.EntryConfig{}
	scheduler := actor.NewScheduler(nil)
	factory := &mockFactory{proc: &mockProcess{}}
	pidGen := newTestPIDGen()
	logger := zap.NewNop()

	h := NewHost(id, cfg, scheduler, factory, pidGen, logger)

	resultPID := h.preparePID(ctxWithPIDGen(), &process.Start{
		Source: registry.NewID("test", "proc"),
	})

	assert.NotEqual(t, pid.PID{}, resultPID)
	assert.Equal(t, pid.NodeID("test-node"), resultPID.Node)
}

var _ process.Host = (*Host)(nil)
