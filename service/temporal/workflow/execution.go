package workflow

import (
	"fmt"
	"time"

	clockapi "github.com/wippyai/runtime/api/clock"
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/function"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	workflowapi "github.com/wippyai/runtime/api/runtime/workflow"
	"github.com/wippyai/runtime/api/topology"
	temporalerrors "github.com/wippyai/runtime/service/temporal/errors"
	commonpb "go.temporal.io/api/common/v1"
	bindings "go.temporal.io/sdk/internalbindings"
	"go.uber.org/zap"
)

// OnWorkflowTaskStarted implements WorkflowDefinition.OnWorkflowTaskStarted.
func (d *Definition) OnWorkflowTaskStarted(_ time.Duration) {
	for {
		var events []process.Event
		if len(d.signals) > 0 {
			for _, sig := range d.signals {
				pkg := &relay.Package{
					Messages: []*relay.Message{{
						Topic:    sig.Name,
						Payloads: sig.Payloads,
					}},
				}
				if sig.From.UniqID != "" || sig.From.Host != "" || sig.From.Node != "" {
					pkg.Source = sig.From
				}
				events = append(events, process.Event{
					Type: process.EventMessage,
					Data: pkg,
				})
			}
			d.signals = d.signals[:0]
		}

		if len(d.childExits) > 0 {
			for _, exit := range d.childExits {
				exitEvent := &topology.ExitEvent{
					At:   d.env.Now(),
					Kind: topology.Exit,
					From: exit.ChildPID,
					Result: &runtime.Result{
						Value: exit.Result,
						Error: exit.Error,
					},
				}
				events = append(events, process.Event{
					Type: process.EventMessage,
					Data: &relay.Package{
						Messages: []*relay.Message{{
							Topic:    topology.TopicEvents,
							Payloads: payload.Payloads{payload.New(exitEvent)},
						}},
					},
				})
			}
			d.childExits = d.childExits[:0]
		}

		if d.updates.HasPending() {
			pending := d.updates.FlushPending()
			d.replayLog.Debug("processing pending updates", zap.Int("count", len(pending)))
			for _, upd := range pending {
				updatePID := pid.PID{Host: pidHostUpdate, UniqID: upd.ID}
				events = append(events, process.Event{
					Type: process.EventMessage,
					Data: &relay.Package{
						Source: updatePID,
						Messages: []*relay.Message{{
							Topic:    upd.Name,
							Payloads: upd.Payloads,
						}},
					},
				})
			}
		}

		d.output.Reset()
		err := d.proc.Step(events, &d.output)
		d.replayLog.Debug("proc.Step", zap.Int("status", int(d.output.Status())), zap.Error(err))

		if d.result != nil {
			d.completeWithResult()
			return
		}

		if err != nil {
			d.env.Complete(nil, temporalerrors.ToApplicationError(err))
			return
		}

		switch d.output.Status() {
		case process.StepYield:
			d.output.ForEachYield(func(y process.Yield) {
				if d.completed {
					return
				}
				if err := d.executeCommand(y.Cmd, y.Tag); err != nil {
					d.completed = true
					d.env.Complete(nil, temporalerrors.ToApplicationError(err))
				}
			})
			return

		case process.StepDone:
			if result := d.output.Result(); result != nil {
				res, err := d.dc.ToPayloads(payload.Payloads{result})
				if err != nil {
					d.env.Complete(nil, fmt.Errorf("failed to encode result: %w", err))
					return
				}
				d.env.Complete(res, nil)
			} else {
				d.env.Complete(nil, nil)
			}
			return

		case process.StepIdle:
			return

		case process.StepUpgrade:
			d.executeContinueAsNew()
			return

		case process.StepContinue:
			d.output.ForEachYield(func(y process.Yield) {
				if d.completed {
					return
				}
				if err := d.executeCommand(y.Cmd, y.Tag); err != nil {
					d.completed = true
					d.env.Complete(nil, temporalerrors.ToApplicationError(err))
				}
			})
		}
	}
}

// executeContinueAsNew handles process.upgrade() by triggering Temporal's ContinueAsNew.
func (d *Definition) executeContinueAsNew() {
	req := d.output.Upgrade()
	if req == nil {
		d.env.Complete(nil, fmt.Errorf("upgrade: no request"))
		return
	}

	workflowType := d.id.String()
	if req.Source.Name != "" {
		workflowType = req.Source.String()
	}

	var input *commonpb.Payloads
	if len(req.Input) > 0 {
		var err error
		input, err = d.dc.ToPayloads(req.Input)
		if err != nil {
			d.env.Complete(nil, fmt.Errorf("upgrade: failed to encode input: %w", err))
			return
		}
	}

	continueErr := &bindings.ContinueAsNewError{
		WorkflowType:  &bindings.WorkflowType{Name: workflowType},
		Input:         input,
		Header:        d.getContextHeader(),
		TaskQueueName: d.env.WorkflowInfo().TaskQueueName,
	}

	d.env.Complete(nil, continueErr)
}

func (d *Definition) executeCommand(cmd dispatcher.Command, tag uint64) error {
	switch c := cmd.(type) {
	case clockapi.SleepCmd:
		return d.executeSleep(c, tag)
	case clockapi.TimerStartCmd:
		return d.executeTimerStart(c, tag)
	case clockapi.TimerStopCmd:
		return d.executeTimerStop(c, tag)
	case clockapi.TimerResetCmd:
		return d.executeTimerReset(c, tag)
	case clockapi.TickerStartCmd:
		return d.executeTickerStart(c, tag)
	case clockapi.TickerStopCmd:
		return d.executeTickerStop(c, tag)
	case *function.CallCmd:
		return d.executeFunctionCall(c, tag)
	case *process.SendCmd:
		return d.executeProcessSend(c, tag)
	case *process.SpawnCmd:
		return d.executeProcessSpawn(c, tag)
	case *process.TerminateCmd:
		return d.executeProcessTerminate(c, tag)
	case *process.CancelCmd:
		return d.executeProcessCancel(c, tag)
	case *process.MonitorCmd:
		return d.executeProcessMonitor(c, tag)
	case *process.UnmonitorCmd:
		return d.executeProcessUnmonitor(c, tag)
	case *process.LinkCmd:
		return d.executeProcessLink(c, tag)
	case *process.UnlinkCmd:
		return d.executeProcessUnlink(c, tag)
	case *process.ExecCmd:
		return d.executeProcessExec(c, tag)
	case *workflowapi.SideEffectCmd:
		return d.executeSideEffect(c, tag)
	case *workflowapi.ExecCmd:
		return d.executeWorkflowExec(c, tag)
	case *workflowapi.VersionCmd:
		return d.executeVersion(c, tag)
	case *workflowapi.UpsertAttrsCmd:
		return d.executeUpsertAttrs(c, tag)
	default:
		return fmt.Errorf("unknown command type: %T", cmd)
	}
}

func (d *Definition) resumeProcess(tag uint64, data any, err error) {
	events := []process.Event{{
		Type:  process.EventYieldComplete,
		Tag:   tag,
		Data:  data,
		Error: err,
	}}
	d.output.Reset()
	stepErr := d.proc.Step(events, &d.output)

	if stepErr != nil {
		d.result = &runtime.Result{Error: stepErr}
		d.completeWithResult()
		return
	}

	switch d.output.Status() {
	case process.StepDone:
		d.result = &runtime.Result{Value: d.output.Result()}
		d.completeWithResult()
	case process.StepYield:
		d.output.ForEachYield(func(y process.Yield) {
			if d.completed {
				return
			}
			if err := d.executeCommand(y.Cmd, y.Tag); err != nil {
				d.completed = true
				d.env.Complete(nil, temporalerrors.ToApplicationError(err))
			}
		})
	case process.StepUpgrade:
		d.executeContinueAsNew()
	case process.StepContinue, process.StepIdle:
		// Process needs more events to continue, nothing to do here
	}
}
