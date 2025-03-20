package btea

import (
	"errors"
	luaconv "github.com/ponyruntime/pony/system/payload/lua"
	"time"

	task2 "github.com/ponyruntime/pony/runtime/lua/task"

	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/runtime"
	"github.com/ponyruntime/pony/runtime/lua/engine/subscribe"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

var (
	ErrTimeout = errors.New("task timeout")
)

// TaskRunner handles task management for btea apps
type TaskRunner struct {
	// The app instance
	app *App

	// Lua state
	state *lua.LState

	// Transcoder for payload conversion
	dtt payload.Transcoder

	// Logger
	log *zap.Logger
}

// NewTaskRunner creates a new task runner for a btea app
func NewTaskRunner(app *App) *TaskRunner {
	return &TaskRunner{
		app:   app,
		state: app.state.UoW.State(),
		dtt:   payload.GetTranscoder(app.state.Ctx),
		log:   app.state.Log,
	}
}

// SendTask sends a task to the specified channel without waiting for response
func (r *TaskRunner) SendTask(taskType string, input lua.LValue) error {
	// Create payload
	inputPayload := luaconv.ExportPayload(input)

	// Create task without completion callback
	t := task2.NewTask(inputPayload, nil)

	// Create message table
	msg := r.state.CreateTable(0, 2)
	msg.RawSetString("type", lua.LString(taskType))
	msg.RawSetString("task", task2.WrapTask(r.state, t))

	// Publish to events channel
	return subscribe.Publish(r.app.state.Ctx, ChannelEvents, msg)
}

// ExecuteTask creates and executes a task, waiting for a result with timeout
func (r *TaskRunner) ExecuteTask(taskType string, input lua.LValue, timeout time.Duration) (string, error) {
	// Check app cancellation signals
	select {
	case <-r.app.state.Ctx.Done():
		return "context error", r.app.state.Ctx.Err()
	case <-r.app.appCtx.Done():
		return "app context canceled", errors.New("app context canceled")
	case <-r.app.done:
		return "done", errors.New("app done")
	default:
		// Continue
	}

	// For fire-and-forget (timeout = 0)
	if timeout <= 0 {
		err := r.SendTask(taskType, input)
		if err != nil {
			r.log.Error("failed to send task", zap.String("task", taskType), zap.Error(err))
		}
		return "", err
	}

	// Create input payload
	inputPayload := luaconv.ExportPayload(input)

	// Create result channel
	resultCh := make(chan runtime.Result, 1)

	// Create task with completion callback
	t := task2.NewTask(inputPayload, func(result runtime.Result) {
		select {
		case resultCh <- result:
			// Result sent
		default:
			// Channel might be closed or full
		}
	})

	// Create message table
	msg := r.state.CreateTable(0, 2)
	msg.RawSetString("type", lua.LString(taskType))
	msg.RawSetString("task", task2.WrapTask(r.state, t))

	// Publish to events channel
	if err := subscribe.Publish(r.app.state.Ctx, ChannelEvents, msg); err != nil {
		r.log.Error("failed to publish task", zap.String("task", taskType), zap.Error(err))
		return "", err
	}

	// Wait for result with timeout
	var result runtime.Result
	select {
	case result = <-resultCh:
		// Got result
	case <-time.After(timeout):
		r.log.Debug("task timeout", zap.String("task", taskType))
		return "", ErrTimeout
	case <-r.app.done:
		return "task canceled", errors.New("task canceled")
	case <-r.app.state.Ctx.Done():
		return "task canceled", r.app.state.Ctx.Err()
	case <-r.app.appCtx.Done():
		return "app canceled", errors.New("app canceled")
	}

	// Handle error in result
	if result.Error != nil {
		r.log.Error("task failed", zap.String("task", taskType), zap.Error(result.Error))
		return result.Error.Error(), result.Error
	}

	// Format result as string
	resultStr := r.formatResult(result.Value)

	// Wake up the unit of work to process pending tasks
	r.app.state.UoW.Tasks().WakeUp()

	return resultStr, nil
}

// formatResult converts a payload to string
func (r *TaskRunner) formatResult(value payload.Payload) string {
	// Handle nil case
	if value == nil {
		return ""
	}

	// Handle Lua values directly
	if value.Format() == payload.Lua {
		if lv, ok := value.Data().(lua.LValue); ok {
			return lv.String()
		}
	}

	// Try transcoding if available
	if r.dtt != nil {
		strPayload, err := r.dtt.Transcode(value, payload.String)
		if err == nil && strPayload != nil {
			if str, ok := strPayload.Data().(string); ok {
				return str
			}
		}
	}

	return "non-string result"
}
