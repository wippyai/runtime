package tasker

import (
	"context"
	"fmt"
	"github.com/ponyruntime/pony/internal/closer"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
	"log"
	"sync"
	"sync/atomic"
)

// Tasker manages task execution within a Lua VM
type Tasker struct {
	log     *zap.Logger
	cvm     *engine.CoroutineVM
	inbox   chan *taskSchedule
	running atomic.Bool
	runner  *engine.Runner
	wg      sync.WaitGroup
}

// NewTasker creates a new instance of the task manager
func NewTasker(
	log *zap.Logger,
	cvm *engine.CoroutineVM,
	channels *channel.Layer,
	inboxSize int,
	opts ...engine.RunnerOption,
) *Tasker {
	inbox := make(chan *taskSchedule, inboxSize)
	mixer := newTaskMixer(channels, inbox)

	// Set up base layers and add user options
	baseOpts := []engine.RunnerOption{
		engine.WithLayer(channels),
		engine.WithLayer(mixer),
	}

	return &Tasker{
		log:    log,
		cvm:    cvm,
		inbox:  inbox,
		runner: engine.NewRunner(cvm, append(baseOpts, opts...)...),
	}
}

// Start initiates the task manager service
func (t *Tasker) Start(ctx context.Context, funcName string, args ...lua.LValue) (<-chan any, error) {
	if !t.running.CompareAndSwap(false, true) {
		return nil, fmt.Errorf("tasker already running")
	}

	ctx, cleanup := closer.WithContext(t.runner.WithContext(ctx))
	log.Printf("cleanup: %v", ctx)
	defer func() {
		if err := cleanup.Close(); err != nil {
			t.log.Error("cleanup failed", zap.Error(err))
		}
	}()

	status := make(chan any, 9)

	// Start the engine execution
	exitCh, err := t.runner.Start(ctx, funcName, args...)
	if err != nil {
		t.running.Store(false)
		return nil, fmt.Errorf("failed to start engine: %v", err)
	}

	status <- "engine started"

	// Start the main execution loop
	t.wg.Add(1)
	go func() {
		defer t.wg.Done()
		defer t.running.Store(false)
		defer close(status)

		// Run the engine with context
		result, err := t.runner.Run(ctx, exitCh)
		if err != nil {
			status <- fmt.Sprintf("engine error: %v", err)
			return
		}
		status <- fmt.Sprintf("engine exit: %v", result)
	}()

	return status, nil
}

// Execute submits a new task for execution
func (t *Tasker) Execute(ctx context.Context, id TaskID, input []lua.LValue) (<-chan engine.Result, error) {
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
	case t.inbox <- schedule:
		t.runner.GetTaskGroup().WakeUp()
		return resultChan, nil
	case <-ctx.Done():
		close(resultChan)
		return nil, ctx.Err()
	}
}

// Stop gracefully shuts down the task manager
func (t *Tasker) Stop(ctx context.Context) error {
	if !t.running.Load() {
		return nil
	}

	// Close inbox to signal shutdown
	close(t.inbox)

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
		return ctx.Err()
	}
}
