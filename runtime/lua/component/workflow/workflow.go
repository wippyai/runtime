package workflow

import (
	"context"
	"errors"
	"fmt"

	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/supervisor"
	"github.com/wippyai/runtime/api/workflow/std"
	baseprocess "github.com/wippyai/runtime/runtime/lua/component/process"
	"github.com/wippyai/runtime/runtime/lua/engine/subscribe"
	"github.com/wippyai/runtime/runtime/lua/modules/upstream"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

// workflowUpstream implements runtime.Upstream for workflow command queuing
type workflowUpstream struct {
	commands []runtime.Command
}

func (u *workflowUpstream) SendRequest(cmd runtime.Command) error {
	u.commands = append(u.commands, cmd)
	return nil
}

func (u *workflowUpstream) FlushRequests() []runtime.Command {
	cmds := u.commands
	u.commands = nil
	return cmds
}

// LuaWorkflow wraps a Lua process state for workflow execution
type LuaWorkflow struct {
	*baseprocess.State
	log      *zap.Logger
	upstream *workflowUpstream
}

// NewLuaWorkflow creates a new Lua workflow from a runner
func NewLuaWorkflow(log *zap.Logger, state *baseprocess.State) *LuaWorkflow {
	return &LuaWorkflow{
		State:    state,
		log:      log,
		upstream: &workflowUpstream{},
	}
}

// Commands returns all pending requests from the upstream handler
func (w *LuaWorkflow) Commands() []runtime.Command {
	if w.upstream == nil {
		return nil
	}
	return w.upstream.FlushRequests()
}

// Start initializes the workflow with context, PID, and input payloads
func (w *LuaWorkflow) Start(ctx context.Context, pid relay.PID, input payload.Payloads) error {
	// Attach upstream handler to the context before initializing
	if err := runtime.WithUpstream(ctx, w.upstream); err != nil {
		return fmt.Errorf("failed to attach upstream handler: %w", err)
	}

	if err := w.InitContext(ctx, pid); err != nil {
		return err
	}

	return w.State.Start(input, nil)
}

// Step advances the workflow state by one iteration
func (w *LuaWorkflow) Step() (process.StepResult, error) {
	err := w.State.Step(false)

	if err != nil {
		if errors.Is(err, supervisor.ErrExit) {
			return process.StepDone, nil
		}
		return process.StepContinue, err
	}

	if w.GetTaskCount() > 0 {
		return process.StepContinue, nil
	}

	return process.StepIdle, nil
}

// Send implements relay.Receiver by delegating to State.SendPackage
func (w *LuaWorkflow) Send(pkg *relay.Package) error {
	return w.SendPackage(pkg)
}

// Terminate implements process.Process
func (w *LuaWorkflow) Terminate() {
	w.Complete(process.ErrTerminated, lua.LNil)
}

// TopicTasks is the internal topic for workflow tasks
const TopicTasks = "@workflow/tasks"

// PushTask implements workflow.TaskReceiver
func (w *LuaWorkflow) PushTask(task std.Task) error {
	if w.UoW == nil {
		return fmt.Errorf("workflow not started")
	}

	ctx := w.UoW.Context()
	state := w.UoW.State()
	if state == nil {
		return fmt.Errorf("no lua state available")
	}

	// Wrap task as userdata and publish to tasks topic
	wrappedTask := upstream.WrapTask(state, task)
	return subscribe.Publish(ctx, TopicTasks, wrappedTask)
}
