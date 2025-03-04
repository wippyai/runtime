package process

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"

	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/process"
	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/runtime"
	"github.com/ponyruntime/pony/api/supervisor"
	"github.com/ponyruntime/pony/api/topology"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/subscribe"
	processmod "github.com/ponyruntime/pony/runtime/lua/modules/process"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

// Common errors
var (
	ErrRunnerRequired = errors.New("runner is required")
	ErrNoTranscoder   = errors.New("failed to get transcoder")
	ErrNoProcess      = errors.New("no process running")
	ErrProcessClosed  = errors.New("process already closed")
	ErrLinkDown       = errors.New("linked process terminated abnormally")
)

// State holds the state data common to all Lua processes
type State struct {
	// Core dependencies
	Log      *zap.Logger
	Runner   *engine.Runner
	FuncName string

	// Context and process information
	Ctx    context.Context
	DTT    payload.Transcoder
	Cancel context.CancelFunc
	UoW    engine.UnitOfWork
	PID    pubsub.PID

	// State tracking
	wg        sync.WaitGroup
	resultCh  <-chan *engine.Update
	Closed    atomic.Bool
	trapExit  atomic.Bool  // Controls whether process traps exit signals
	exitError atomic.Value // Holds an error that should terminate the process on next step
}

// NewState creates a new process state
func NewState(
	log *zap.Logger,
	runner *engine.Runner,
	funcName string,
) (*State, error) {
	if runner == nil {
		return nil, ErrRunnerRequired
	}

	if log == nil {
		log = zap.NewNop()
	}

	s := &State{
		Log:      log,
		Runner:   runner,
		FuncName: funcName,
	}

	return s, nil
}

// InitContext initializes the process context and unit of work
func (s *State) InitContext(ctx context.Context, pid pubsub.PID) error {
	s.Ctx, s.Cancel = context.WithCancel(ctx)
	s.PID = pid

	// Get transcoder
	s.DTT = payload.GetTranscoder(ctx)
	if s.DTT == nil {
		return ErrNoTranscoder
	}

	// Set PID in context
	ctx = pubsub.WithPID(ctx, pid)

	// Init unit of work
	s.UoW, s.Ctx = s.Runner.InitUnitOfWork(ctx)

	// We expect Ctx being overwritten by parent caller

	return nil
}

// SetTrapExit enables or disables trapping of exit signals from linked processes
func (s *State) SetTrapExit(trap bool) {
	s.trapExit.Store(trap)
}

// IsTrapExitEnabled returns whether the process is trapping exit signals
func (s *State) IsTrapExitEnabled() bool {
	return s.trapExit.Load()
}

// ToLuaPayloads converts a slice of payloads to Lua values
func (s *State) ToLuaPayloads(payloads payload.Payloads) ([]lua.LValue, error) {
	args := make([]lua.LValue, 0, len(payloads))
	for _, pp := range payloads {
		luaPayload, err := s.DTT.Transcode(pp, payload.Lua)
		if err != nil {
			return nil, err
		}

		if lv, ok := luaPayload.Data().(lua.LValue); ok {
			args = append(args, lv)
		}
	}

	return args, nil
}

// Start initializes the Lua process and starts execution
func (s *State) Start(input payload.Payloads, onStart func()) error {
	// Convert input payloads to Lua values
	args, err := s.ToLuaPayloads(input)
	if err != nil {
		return err
	}

	// Start the Lua function
	s.resultCh, err = s.Runner.Start(s.Ctx, s.FuncName, args...)
	if err != nil {
		return err
	}

	onStart()

	// Handle the initial result if any using select
	select {
	case <-s.Ctx.Done():
		return s.Ctx.Err()
	default:
		return nil
	case result := <-s.resultCh:
		if result.Error != nil {
			s.Complete(result.Error, nil)
			return result.Error
		}
		if len(result.Result) > 0 {
			// Process completed immediately
			s.Complete(nil, result.Result[0])
			return supervisor.ErrExit
		}
	}

	return nil
}

// Step advances the process state by one iteration
func (s *State) Step() error {
	s.wg.Add(1)
	defer s.wg.Done()

	// Check for context cancellation or already closed
	if s.Ctx.Err() != nil || s.Closed.Load() {
		return s.Ctx.Err()
	}

	// Check for pending exit error from link down events and terminates
	if err := s.getExitError(); err != nil {
		s.Complete(err, nil)
		return err
	}

	// Continue the runner
	if err := s.Runner.Continue(s.Ctx, s.UoW.Tasks().Ready() == 0); err != nil {
		s.Complete(err, nil)
		return err
	}

	// Check for any results
	select {
	case result := <-s.resultCh:
		if result.Error != nil {
			s.Complete(result.Error, nil)
			return result.Error
		}
		if len(result.Result) > 0 {
			s.Complete(nil, result.Result[0])
			return supervisor.ErrExit
		}
	default:
		// No result yet, continue
	}

	return nil
}

// setExitError sets an error that will cause the process to exit on the next step.
// This also triggers a wake-up to ensure the error is processed promptly.
func (s *State) setExitError(err error) {
	if err == nil {
		return
	}

	s.exitError.Store(err)

	// Wake up the unit of work to process this error ASAP
	if s.UoW != nil && s.UoW.Tasks() != nil {
		s.UoW.Tasks().WakeUp()
	}
}

// getExitError retrieves and clears any pending exit error
func (s *State) getExitError() error {
	val := s.exitError.Load()
	if val == nil {
		return nil
	}

	// Clear the error so we don't process it twice
	s.exitError.Store(nil)

	if err, ok := val.(error); ok {
		return err
	}

	return nil
}

// GetTaskCount returns the combined count of ready tasks
func (s *State) GetTaskCount() int {
	if s.UoW == nil || s.Runner == nil {
		return 0
	}

	return s.UoW.Tasks().Ready() + s.Runner.QueueLen()
}

// Complete handles process completion and cleanup
func (s *State) Complete(err error, result lua.LValue) {
	// Check if already completed to avoid double cleanup
	if s.Closed.Swap(true) {
		s.Log.Warn("process already completed", zap.String("pid", s.PID.String()))
		return
	}

	// Wait for any pending operations to complete
	s.wg.Wait()

	// Clean up unit of work
	if s.UoW != nil {
		if cErr := s.UoW.Close(); cErr != nil {
			s.Log.Error("failed to close unit of work", zap.Error(cErr))
		}
	}

	// Notify onComplete handlers
	if onComplete := process.GetOnComplete(s.Ctx); onComplete != nil {
		if err != nil {
			onComplete(s.PID, &runtime.Result{Error: err})
		} else {
			onComplete(s.PID, &runtime.Result{
				Payload: payload.NewPayload(result, payload.Lua),
			})
		}
	}

	// Clean up resources
	s.Runner.Close()
	s.Cancel()
	s.UoW = nil
	s.Runner = nil
}

// ProcessPackage handles an incoming message package
func (s *State) ProcessPackage(pkg *pubsub.Package) error {
	s.wg.Add(1)
	defer s.wg.Done()

	if s.Ctx.Err() != nil || s.Closed.Load() {
		return s.Ctx.Err()
	}

	if pkg == nil {
		return errors.New("package is nil")
	}

	select {
	case <-s.Ctx.Done():
		return s.Ctx.Err()
	default:
		for _, msg := range pkg.Messages {
			if msg.Topic == topology.TopicEvents && s.handleTopologyMessage(msg) {
				// handled level above
				continue
			}

			// Check if the topic has a specific channel
			if exists, _ := subscribe.Exists(s.Ctx, msg.Topic); exists {
				// Convert payloads to Lua values
				luaValues, err := s.ToLuaPayloads(msg.Payloads)
				if err != nil {
					s.Log.Error("failed to convert payloads", zap.Error(err))
					continue
				}

				if len(luaValues) == 0 {
					continue
				}

				if err := subscribe.Publish(s.Ctx, msg.Topic, luaValues...); err != nil {
					s.Log.Error("failed to publish message",
						zap.String("topic", msg.Topic),
						zap.Error(err))
				}
				continue
			}

			hasInbox, err := subscribe.Exists(s.Ctx, topology.TopicInbox)
			if err != nil {
				s.Log.Error("failed to check inbox",
					zap.String("topic", topology.TopicInbox),
					zap.Error(err))
				continue
			}

			if hasInbox {
				inboxValues := make([]lua.LValue, 0, len(msg.Payloads))

				for _, p := range msg.Payloads {
					m := processmod.NewMessage(msg.Topic, p)
					inboxValues = append(inboxValues, processmod.WrapMessage(s.UoW.State(), m))
				}

				if err := subscribe.Publish(s.Ctx, topology.TopicInbox, inboxValues...); err != nil {
					s.Log.Error("failed to publish to inbox",
						zap.String("topic", topology.TopicInbox),
						zap.Error(err))
				}
			}
		}

		pubsub.ReleasePackage(pkg)
		return nil
	}
}

// handleTopologyEvent processes a topology event and takes appropriate action
func (s *State) handleTopologyMessage(msg *pubsub.Message) bool {
	if len(msg.Payloads) != 1 {
		// topology events should have exactly one payload
		return false
	}

	data := msg.Payloads[0].Data()

	// Handle different event types
	switch event := data.(type) {
	case *topology.ExitEvent:
		if event.Event.Kind == topology.KindLinkDown {
			// Link down event - terminate if not trapping
			var exitErr error
			if event.Result != nil && event.Result.Error != nil {
				exitErr = event.Result.Error
			} else {
				exitErr = ErrLinkDown
			}

			s.Log.Debug("link down detected, setting exit error",
				zap.String("pid", s.PID.String()),
				zap.String("from", event.Event.From.String()),
				zap.Error(exitErr))

			if !s.IsTrapExitEnabled() {
				s.setExitError(exitErr)
				return true
			}
		}

		// todo: we can also process link events to know who is linked to us
	}

	return false
}
