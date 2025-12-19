package host

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
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
	"github.com/wippyai/runtime/api/topology"
	"github.com/wippyai/runtime/internal/uniqid"
	"github.com/wippyai/runtime/system/scheduler/actor"
	"go.uber.org/zap"
)

// --- Test Infrastructure ---

func newTestPIDGen() *uniqid.PIDGenerator {
	return uniqid.NewPIDGenerator(uniqid.NewGenerator(), "test-node")
}

func ctxWithAppContext() context.Context {
	appCtx := ctxapi.NewAppContext()
	return ctxapi.WithAppContext(context.Background(), appCtx)
}

// mockProcess implements process.Process for testing.
type mockProcess struct {
	initErr  error
	stepFunc func([]process.Event, *process.StepOutput) error
}

func (m *mockProcess) Init(_ context.Context, _ string, _ payload.Payloads) error {
	return m.initErr
}

func (m *mockProcess) Step(events []process.Event, out *process.StepOutput) error {
	if m.stepFunc != nil {
		return m.stepFunc(events, out)
	}
	out.Done(nil)
	return nil
}

func (m *mockProcess) Close() {}

// mockFactory implements process.Factory for testing.
type mockFactory struct {
	proc   process.Process
	meta   *process.Meta
	err    error
	called atomic.Int32
}

func (f *mockFactory) Create(_ registry.ID) (process.Process, *process.Meta, error) {
	f.called.Add(1)
	if f.err != nil {
		return nil, nil, f.err
	}
	return f.proc, f.meta, nil
}

// mockPIDRegistry implements topology.PIDRegistry for testing.
type mockPIDRegistry struct {
	mu      sync.Mutex
	pids    map[string]pid.PID
	removed []pid.PID
}

func newMockPIDRegistry() *mockPIDRegistry {
	return &mockPIDRegistry{pids: make(map[string]pid.PID)}
}

func (r *mockPIDRegistry) Register(name string, p pid.PID) (pid.PID, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.pids[name]; ok {
		return existing, errors.New("name taken")
	}
	r.pids[name] = p
	return p, nil
}

func (r *mockPIDRegistry) Unregister(name string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.pids[name]
	delete(r.pids, name)
	return ok
}

func (r *mockPIDRegistry) Lookup(name string) (pid.PID, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.pids[name]
	return p, ok
}

func (r *mockPIDRegistry) Remove(p pid.PID) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.removed = append(r.removed, p)
}

// mockLifecycle implements process.Lifecycle for testing.
type mockLifecycle struct {
	onStartErr  error
	onStartFunc func(context.Context, pid.PID, process.Process)
	onComplete  func(context.Context, pid.PID, *runtime.Result)
}

func (m *mockLifecycle) OnStart(ctx context.Context, p pid.PID, proc process.Process) error {
	if m.onStartFunc != nil {
		m.onStartFunc(ctx, p, proc)
	}
	return m.onStartErr
}

func (m *mockLifecycle) OnComplete(ctx context.Context, p pid.PID, result *runtime.Result) {
	if m.onComplete != nil {
		m.onComplete(ctx, p, result)
	}
}

// testHost creates a configured Host for testing.
type testHost struct {
	host      *Host
	scheduler *actor.Scheduler
	factory   *mockFactory
	pidReg    *mockPIDRegistry
	lifecycle *mockLifecycle
}

func newTestHost(opts ...func(*testHost)) *testHost {
	th := &testHost{
		factory:   &mockFactory{proc: &mockProcess{}},
		pidReg:    newMockPIDRegistry(),
		lifecycle: &mockLifecycle{},
	}

	for _, opt := range opts {
		opt(th)
	}

	th.scheduler = actor.NewScheduler(nil, actor.WithLifecycle(th.lifecycle))
	th.host = NewHost(
		registry.NewID("test", "host"),
		&hostapi.EntryConfig{},
		th.scheduler,
		th.factory,
		newTestPIDGen(),
		zap.NewNop(),
		WithPIDRegistry(th.pidReg),
	)

	return th
}

func (th *testHost) start(t *testing.T) {
	_, err := th.host.Start(ctxWithAppContext())
	require.NoError(t, err)
}

func (th *testHost) stop() {
	_ = th.host.Stop(context.Background())
}

// --- Host Construction Tests ---

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
	assert.Nil(t, h.pidReg)
}

func TestWithPIDRegistry(t *testing.T) {
	pidReg := newMockPIDRegistry()
	th := newTestHost(func(th *testHost) {
		th.pidReg = pidReg
	})

	assert.Equal(t, pidReg, th.host.pidReg)
}

// --- Host Start/Stop Tests ---

func TestHost_Start(t *testing.T) {
	th := newTestHost()

	ch, err := th.host.Start(ctxWithAppContext())
	require.NoError(t, err)
	assert.Nil(t, ch)
	assert.True(t, th.host.running.Load())

	th.stop()
}

func TestHost_StartAlreadyRunning(t *testing.T) {
	th := newTestHost()
	th.start(t)
	defer th.stop()

	_, err := th.host.Start(context.Background())
	assert.ErrorIs(t, err, ErrHostAlreadyRunning)
}

func TestHost_Stop(t *testing.T) {
	th := newTestHost()
	th.start(t)

	err := th.host.Stop(context.Background())
	require.NoError(t, err)

	assert.False(t, th.host.running.Load())
	assert.True(t, th.host.shutdown.Load())
}

func TestHost_StopNotRunning(t *testing.T) {
	th := newTestHost()

	err := th.host.Stop(context.Background())
	assert.NoError(t, err)
}

func TestHost_StopIdempotent(t *testing.T) {
	th := newTestHost()
	th.start(t)

	err := th.host.Stop(context.Background())
	require.NoError(t, err)

	err = th.host.Stop(context.Background())
	assert.NoError(t, err)
}

// --- Host Run Tests ---

func TestHost_RunNotRunning(t *testing.T) {
	th := newTestHost()

	_, err := th.host.Run(context.Background(), &process.Start{
		Source: registry.NewID("test", "proc"),
	})

	assert.ErrorIs(t, err, ErrHostNotRunning)
}

func TestHost_RunShuttingDown(t *testing.T) {
	th := newTestHost()
	th.host.running.Store(true)
	th.host.shutdown.Store(true)

	_, err := th.host.Run(context.Background(), &process.Start{
		Source: registry.NewID("test", "proc"),
	})

	assert.ErrorIs(t, err, ErrHostShuttingDown)
}

func TestHost_RunFactoryError(t *testing.T) {
	factoryErr := errors.New("factory error")
	th := newTestHost(func(th *testHost) {
		th.factory.err = factoryErr
	})
	th.start(t)
	defer th.stop()

	_, err := th.host.Run(ctxWithAppContext(), &process.Start{
		Source: registry.NewID("test", "proc"),
	})

	assert.ErrorIs(t, err, factoryErr)
}

func TestHost_RunSuccess(t *testing.T) {
	th := newTestHost()
	th.start(t)
	defer th.stop()

	resultPID, err := th.host.Run(ctxWithAppContext(), &process.Start{
		Source: registry.NewID("test", "proc"),
	})

	require.NoError(t, err)
	assert.NotEqual(t, pid.PID{}, resultPID)
	assert.Equal(t, "test-node", resultPID.Node)
	assert.Equal(t, int32(1), th.factory.called.Load())
}

func TestHost_RunWithMeta(t *testing.T) {
	th := newTestHost(func(th *testHost) {
		th.factory.meta = &process.Meta{Method: "custom_method"}
	})
	th.start(t)
	defer th.stop()

	resultPID, err := th.host.Run(ctxWithAppContext(), &process.Start{
		Source: registry.NewID("test", "proc"),
	})

	require.NoError(t, err)
	assert.NotEqual(t, pid.PID{}, resultPID)
}

func TestHost_RunWithMessages(t *testing.T) {
	th := newTestHost()
	th.start(t)
	defer th.stop()

	messages := []*relay.Message{
		{Topic: "topic1", Payloads: payload.Payloads{}},
		{Topic: "topic2", Payloads: payload.Payloads{}},
	}

	resultPID, err := th.host.Run(ctxWithAppContext(), &process.Start{
		Source:   registry.NewID("test", "proc"),
		Messages: messages,
	})

	require.NoError(t, err)
	assert.NotEqual(t, pid.PID{}, resultPID)
}

// --- Spawn-or-Signal Tests ---

func TestHost_RunShortcutExistingProcess(t *testing.T) {
	existingPID := pid.PID{Node: "test", Host: "test:host", UniqID: "existing-123"}
	th := newTestHost()
	th.pidReg.pids["my-service"] = existingPID
	th.start(t)
	defer th.stop()

	resultPID, err := th.host.Run(ctxWithAppContext(), &process.Start{
		Source: registry.NewID("test", "proc"),
		Name:   "my-service",
	})

	require.NoError(t, err)
	assert.Equal(t, existingPID, resultPID)
	assert.Equal(t, int32(0), th.factory.called.Load()) // factory not called
}

func TestHost_RunShortcutWithMessages(t *testing.T) {
	existingPID := pid.PID{Node: "test", Host: "test:host", UniqID: "existing-456"}
	th := newTestHost()
	th.pidReg.pids["my-service"] = existingPID
	th.start(t)
	defer th.stop()

	messages := []*relay.Message{{Topic: "hello"}}
	resultPID, err := th.host.Run(ctxWithAppContext(), &process.Start{
		Source:   registry.NewID("test", "proc"),
		Name:     "my-service",
		Messages: messages,
	})

	require.NoError(t, err)
	assert.Equal(t, existingPID, resultPID)
	assert.Equal(t, int32(0), th.factory.called.Load())
}

func TestHost_RunNameTakenAfterSpawn(t *testing.T) {
	existingPID := pid.PID{Node: "test", Host: "test:host", UniqID: "winner"}
	th := newTestHost(func(th *testHost) {
		th.lifecycle.onStartErr = topology.NameAlreadyRegisteredError(existingPID)
	})
	th.start(t)
	defer th.stop()

	resultPID, err := th.host.Run(ctxWithAppContext(), &process.Start{
		Source: registry.NewID("test", "proc"),
		Name:   "contested-name",
	})

	require.NoError(t, err)
	assert.Equal(t, existingPID, resultPID)
}

func TestHost_RunNameTakenWithMessages(t *testing.T) {
	existingPID := pid.PID{Node: "test", Host: "test:host", UniqID: "winner"}
	th := newTestHost(func(th *testHost) {
		th.lifecycle.onStartErr = topology.NameAlreadyRegisteredError(existingPID)
	})
	th.start(t)
	defer th.stop()

	messages := []*relay.Message{{Topic: "routed"}}
	resultPID, err := th.host.Run(ctxWithAppContext(), &process.Start{
		Source:   registry.NewID("test", "proc"),
		Name:     "contested-name",
		Messages: messages,
	})

	require.NoError(t, err)
	assert.Equal(t, existingPID, resultPID)
}

func TestHost_RunNoPIDRegistry(t *testing.T) {
	scheduler := actor.NewScheduler(nil)
	h := NewHost(
		registry.NewID("test", "host"),
		&hostapi.EntryConfig{},
		scheduler,
		&mockFactory{proc: &mockProcess{}},
		newTestPIDGen(),
		zap.NewNop(),
		// No WithPIDRegistry
	)

	_, err := h.Start(ctxWithAppContext())
	require.NoError(t, err)
	defer h.Stop(context.Background())

	// Should not try shortcut without PIDRegistry
	resultPID, err := h.Run(ctxWithAppContext(), &process.Start{
		Source: registry.NewID("test", "proc"),
		Name:   "my-service",
	})

	require.NoError(t, err)
	assert.NotEqual(t, pid.PID{}, resultPID)
}

func TestHost_RunSchedulerError(t *testing.T) {
	schedulerErr := errors.New("scheduler submit failed")
	th := newTestHost(func(th *testHost) {
		th.lifecycle.onStartErr = schedulerErr
	})
	th.start(t)
	defer th.stop()

	_, err := th.host.Run(ctxWithAppContext(), &process.Start{
		Source: registry.NewID("test", "proc"),
	})

	assert.ErrorIs(t, err, schedulerErr)
}

func TestHost_RunNameTakenWithoutPID(t *testing.T) {
	// Edge case: ErrNameAlreadyRegistered but GetExistingPID fails
	th := newTestHost(func(th *testHost) {
		th.lifecycle.onStartErr = topology.ErrNameAlreadyRegistered
	})
	th.start(t)
	defer th.stop()

	_, err := th.host.Run(ctxWithAppContext(), &process.Start{
		Source: registry.NewID("test", "proc"),
		Name:   "contested-name",
	})

	// Should return the original error since we can't extract PID
	assert.ErrorIs(t, err, topology.ErrNameAlreadyRegistered)
}

// --- HandleNameTaken Tests ---

func TestHost_HandleNameTaken(t *testing.T) {
	th := newTestHost()

	existingPID := pid.PID{Node: "test", Host: "test:host", UniqID: "existing"}
	start := &process.Start{
		Name:     "my-service",
		Messages: []*relay.Message{{Topic: "test"}},
	}

	resultPID, err := th.host.handleNameTaken(existingPID, start)

	assert.NoError(t, err)
	assert.Equal(t, existingPID, resultPID)
}

func TestHost_HandleNameTakenNoMessages(t *testing.T) {
	th := newTestHost()

	existingPID := pid.PID{Node: "test", Host: "test:host", UniqID: "existing"}
	start := &process.Start{Name: "my-service"}

	resultPID, err := th.host.handleNameTaken(existingPID, start)

	assert.NoError(t, err)
	assert.Equal(t, existingPID, resultPID)
}

// --- Send Tests ---

func TestHost_Send(t *testing.T) {
	th := newTestHost()
	th.start(t)
	defer th.stop()

	err := th.host.Send(&relay.Package{Target: pid.PID{}})
	// Error expected - process doesn't exist, but not shutdown error
	assert.NotErrorIs(t, err, ErrHostShuttingDown)
}

func TestHost_SendShuttingDown(t *testing.T) {
	th := newTestHost()
	th.host.shutdown.Store(true)

	err := th.host.Send(&relay.Package{})
	assert.ErrorIs(t, err, ErrHostShuttingDown)
}

// --- Terminate Tests ---

func TestHost_Terminate(t *testing.T) {
	th := newTestHost()
	th.start(t)
	defer th.stop()

	err := th.host.Terminate(context.Background(), pid.PID{UniqID: "nonexistent"})
	assert.Error(t, err) // process doesn't exist
}

// --- PreparePID Tests ---

func TestHost_PreparePID_Generated(t *testing.T) {
	th := newTestHost()

	resultPID := th.host.preparePID(ctxWithAppContext(), &process.Start{
		Source: registry.NewID("test", "proc"),
	})

	assert.NotEqual(t, pid.PID{}, resultPID)
	assert.Equal(t, "test-node", resultPID.Node)
}

func TestHost_PreparePID_ExplicitPID(t *testing.T) {
	th := newTestHost()

	explicitPID := pid.PID{Node: "custom-node", Host: "test:host", UniqID: "explicit-123"}
	opts := attrs.NewBag()
	opts.Set(process.OptionPID, explicitPID)

	resultPID := th.host.preparePID(ctxWithAppContext(), &process.Start{
		Source:  registry.NewID("test", "proc"),
		Options: opts,
	})

	assert.Equal(t, explicitPID, resultPID)
}

func TestHost_PreparePID_InvalidOptionType(t *testing.T) {
	th := newTestHost()

	opts := attrs.NewBag()
	opts.Set(process.OptionPID, "not-a-pid")

	resultPID := th.host.preparePID(ctxWithAppContext(), &process.Start{
		Source:  registry.NewID("test", "proc"),
		Options: opts,
	})

	// Should fall back to generated PID
	assert.NotEqual(t, pid.PID{}, resultPID)
	assert.Equal(t, "test-node", resultPID.Node)
}

func TestHost_PreparePID_NilOptions(t *testing.T) {
	th := newTestHost()

	resultPID := th.host.preparePID(ctxWithAppContext(), &process.Start{
		Source:  registry.NewID("test", "proc"),
		Options: nil,
	})

	assert.NotEqual(t, pid.PID{}, resultPID)
	assert.Equal(t, "test-node", resultPID.Node)
}

// --- PrepareContext Tests ---

func TestHost_PrepareContext(t *testing.T) {
	th := newTestHost()
	th.start(t)
	defer th.stop()

	sourceID := registry.NewID("test", "proc")
	processID := pid.PID{Node: "test", Host: "test:host", UniqID: "proc-123"}
	opts := attrs.NewBag()
	opts.Set("custom_key", "custom_value")

	start := &process.Start{
		Source:  sourceID,
		Options: opts,
		Context: []ctxapi.Pair{
			{Key: "extra_key", Value: "extra_value"},
		},
	}

	ctx := th.host.prepareContext(ctxWithAppContext(), processID, start)

	fc := ctxapi.FrameFromContext(ctx)
	require.NotNil(t, fc)

	// Verify frame values
	idVal, ok := fc.Get(runtime.FrameIDKey)
	assert.True(t, ok)
	assert.Equal(t, sourceID, idVal)

	pidVal, ok := fc.Get(runtime.FramePIDKey)
	assert.True(t, ok)
	assert.Equal(t, processID, pidVal)

	optsVal, ok := fc.Get(runtime.FrameLifecycleOptionsKey)
	assert.True(t, ok)
	assert.Equal(t, opts, optsVal)

	extraVal, ok := fc.Get("extra_key")
	assert.True(t, ok)
	assert.Equal(t, "extra_value", extraVal)
}

func TestHost_PrepareContextEmptyContext(t *testing.T) {
	th := newTestHost()
	th.start(t)
	defer th.stop()

	start := &process.Start{
		Source: registry.NewID("test", "proc"),
	}

	ctx := th.host.prepareContext(ctxWithAppContext(), pid.PID{UniqID: "test"}, start)

	fc := ctxapi.FrameFromContext(ctx)
	require.NotNil(t, fc)
}

// --- Lifecycle Callback Tests ---

func TestHost_OnStart(t *testing.T) {
	th := newTestHost()

	err := th.host.OnStart(context.Background(), pid.PID{}, &mockProcess{})
	assert.NoError(t, err)
}

func TestHost_OnComplete(t *testing.T) {
	th := newTestHost()
	th.start(t)
	defer th.stop()

	// Create context with frame
	appCtx := ctxapi.NewAppContext()
	ctx := ctxapi.WithAppContext(context.Background(), appCtx)
	ctx, _ = ctxapi.OpenFrameContext(ctx)

	// Should not panic
	th.host.OnComplete(ctx, pid.PID{}, &runtime.Result{})
}

func TestHost_OnCompleteNoFrame(t *testing.T) {
	th := newTestHost()

	// Should not panic with nil frame
	th.host.OnComplete(context.Background(), pid.PID{}, &runtime.Result{})
}

// --- SendMessages Tests ---

func TestHost_SendMessages(t *testing.T) {
	th := newTestHost()
	th.start(t)
	defer th.stop()

	target := pid.PID{Node: "test", Host: "test:host", UniqID: "target"}
	messages := []*relay.Message{
		{Topic: "topic1", Payloads: payload.Payloads{}},
		{Topic: "topic2", Payloads: payload.Payloads{}},
	}

	// Should not panic even if process doesn't exist
	th.host.sendMessages(target, messages)
}

func TestHost_SendMessagesEmpty(t *testing.T) {
	th := newTestHost()
	th.start(t)
	defer th.stop()

	target := pid.PID{Node: "test", Host: "test:host", UniqID: "target"}

	// Should not panic with empty messages
	th.host.sendMessages(target, nil)
}

// --- Concurrent Operation Tests ---

func TestHost_ConcurrentRun(t *testing.T) {
	th := newTestHost()
	th.start(t)
	defer th.stop()

	const numGoroutines = 10
	var wg sync.WaitGroup
	errs := make(chan error, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := th.host.Run(ctxWithAppContext(), &process.Start{
				Source: registry.NewID("test", "proc"),
			})
			if err != nil {
				errs <- err
			}
		}()
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestHost_ConcurrentStartStop(t *testing.T) {
	th := newTestHost()

	var wg sync.WaitGroup
	// Only 2 iterations to keep test fast (scheduler stop has timeout)
	for i := 0; i < 2; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			th.host.Start(ctxWithAppContext())
		}()
		go func() {
			defer wg.Done()
			th.host.Stop(context.Background())
		}()
	}

	wg.Wait()
}

func TestHost_ConcurrentSend(t *testing.T) {
	th := newTestHost()
	th.start(t)
	defer th.stop()

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			th.host.Send(&relay.Package{Target: pid.PID{UniqID: "test"}})
		}()
	}

	wg.Wait()
}

// --- Interface Compliance ---

var _ process.Host = (*Host)(nil)
var _ topology.PIDRegistry = (*mockPIDRegistry)(nil)
var _ process.Lifecycle = (*mockLifecycle)(nil)
