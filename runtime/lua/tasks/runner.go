package tasks

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/internal/closer"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
	"sync"
	"sync/atomic"
)

// TaskID represents a unique identifier for a task
type TaskID = string

// TaskRunner manages task execution within a Lua VM
type TaskRunner struct {
	log     *zap.Logger
	cvm     *engine.CoroutineVM
	inbox   chan *taskSchedule
	running atomic.Bool
	mixer   *mixerLayer
	runner  *engine.Runner
	wg      sync.WaitGroup
	cancel  context.CancelFunc
}

// NewTasker creates a new instance of the task manager
func NewTasker(
	log *zap.Logger,
	cvm *engine.CoroutineVM,
	channels *channel.Layer,
	inboxSize int,
	opts ...engine.RunnerOption,
) *TaskRunner {
	inbox := make(chan *taskSchedule, inboxSize)
	mixer := newTaskMixer(channels, inbox)

	// Set up base layers and add user options
	baseOpts := []engine.RunnerOption{
		engine.WithLayer(channels),
		engine.WithLayer(mixer),
	}

	return &TaskRunner{
		log:    log,
		cvm:    cvm,
		inbox:  inbox,
		mixer:  mixer,
		runner: engine.NewRunner(cvm, append(baseOpts, opts...)...),
	}
}

// Start initiates the task manager service
func (t *TaskRunner) Start(ctx context.Context, funcName string, args ...lua.LValue) (<-chan lua.LValue, error) {
	if !t.running.CompareAndSwap(false, true) {
		return nil, fmt.Errorf("tasker already running")
	}

	ctx, cleanup := closer.WithContext(t.runner.WithContext(ctx))
	defer func() {
		if err := cleanup.Close(); err != nil {
			t.log.Error("cleanup failed", zap.Error(err))
		}
	}()

	resultChan := make(chan lua.LValue, 1)

	// always isolate context
	ctx, t.cancel = context.WithCancel(ctx)

	// Start the engine execution
	exitCh, err := t.runner.Start(ctx, funcName, args...)
	if err != nil {
		t.running.Store(false)
		return nil, fmt.Errorf("failed to start engine: %v", err)
	}

	// Start the main execution loop
	t.wg.Add(1)
	go func() {
		defer t.wg.Done()
		defer t.running.Store(false)
		defer close(resultChan)

		// Run the engine with context
		result, err := t.runner.Run(ctx, exitCh)
		if err != nil {
			return
		}
		resultChan <- result
	}()

	return resultChan, nil
}

// State returns the underlying Lua state
func (t *TaskRunner) State() *lua.LState {
	return t.cvm.State()
}

// Execute submits a new task for execution
func (t *TaskRunner) Execute(ctx context.Context, id TaskID, input []lua.LValue) (<-chan engine.Result, error) {
	if !t.running.Load() {
		return nil, fmt.Errorf("tasker not running")
	}

	resultChan := make(chan engine.Result, 1)
	schedule := &taskSchedule{
		id:      id,
		input:   input,
		channel: resultChan,
	}

	// Try to send task
	select {
	case <-t.mixer.close:
		return nil, fmt.Errorf("tasker closed")
	case t.inbox <- schedule:
		t.runner.GetTaskGroup().WakeUp()
		return resultChan, nil
	case <-ctx.Done():
		close(resultChan)
		return nil, ctx.Err()
	}
}

// Stop gracefully shuts down the task manager
func (t *TaskRunner) Stop(ctx context.Context) error {
	if !t.running.Load() {
		return nil
	}

	// closeChannel inbox to signal shutdown
	err := t.mixer.closeChannel()
	if err != nil {
		return fmt.Errorf("failed to close tasks queue: %v", err)
	}
	t.runner.GetTaskGroup().WakeUp()

	// Wait for processing to complete with context deadline
	done := make(chan struct{})
	go func() {
		t.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-ctx.Done():
		t.cancel()
		return ctx.Err()
	}
}
