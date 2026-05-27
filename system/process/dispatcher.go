// SPDX-License-Identifier: MPL-2.0

package process

import (
	"context"
	"errors"

	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/dispatcher"
	api "github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
	topapi "github.com/wippyai/runtime/api/topology"
	"go.uber.org/zap"
)

// Dispatcher handles process commands.
type Dispatcher struct {
	manager api.Manager
	router  relay.Receiver
	topo    topapi.Topology
	logger  *zap.Logger
}

// NewDispatcher creates a new process dispatcher.
func NewDispatcher(manager api.Manager, router relay.Receiver, topo topapi.Topology, logger *zap.Logger) *Dispatcher {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &Dispatcher{
		manager: manager,
		router:  router,
		topo:    topo,
		logger:  logger,
	}
}

// RegisterAll registers all process command handlers.
func (d *Dispatcher) RegisterAll(register func(id dispatcher.CommandID, h dispatcher.Handler)) {
	register(api.Send, dispatcher.HandlerFunc(d.handleSend))
	register(api.Spawn, dispatcher.HandlerFunc(d.handleSpawn))
	register(api.Terminate, dispatcher.HandlerFunc(d.handleTerminate))
	register(api.Cancel, dispatcher.HandlerFunc(d.handleCancel))
	register(api.Monitor, dispatcher.HandlerFunc(d.handleMonitor))
	register(api.Unmonitor, dispatcher.HandlerFunc(d.handleUnmonitor))
	register(api.Link, dispatcher.HandlerFunc(d.handleLink))
	register(api.Unlink, dispatcher.HandlerFunc(d.handleUnlink))
	register(api.Exec, dispatcher.HandlerFunc(d.handleExec))
}

func (d *Dispatcher) handleSend(_ context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	sendCmd := cmd.(*api.SendCmd)

	msg := relay.AcquireMessage()
	msg.Topic = sendCmd.Topic
	msg.Payloads = sendCmd.Payloads
	pkg := relay.NewMessagePackage(sendCmd.From, sendCmd.To, msg)

	err := d.router.Send(pkg)
	if err != nil {
		d.logger.Debug("send failed",
			zap.String("from", sendCmd.From.String()),
			zap.String("to", sendCmd.To.String()),
			zap.String("topic", sendCmd.Topic),
			zap.Error(err))
	}

	receiver.CompleteYield(tag, api.SendResult{Error: err}, nil)
	return nil
}

func (d *Dispatcher) handleSpawn(ctx context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	spawnCmd := cmd.(*api.SpawnCmd)

	childPID, err := d.manager.Start(ctx, spawnCmd.Start)
	if err != nil {
		receiver.CompleteYield(tag, api.SpawnResult{Error: err}, nil)
		return nil
	}

	receiver.CompleteYield(tag, api.SpawnResult{PID: childPID}, nil)
	return nil
}

func (d *Dispatcher) handleTerminate(ctx context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	terminateCmd := cmd.(*api.TerminateCmd)

	err := d.manager.Terminate(ctx, terminateCmd.Target)
	receiver.CompleteYield(tag, nil, err)
	return nil
}

func (d *Dispatcher) handleCancel(ctx context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	cancelCmd := cmd.(*api.CancelCmd)

	err := d.manager.Cancel(ctx, cancelCmd.From, cancelCmd.Target, cancelCmd.Deadline)
	receiver.CompleteYield(tag, nil, err)
	return nil
}

func (d *Dispatcher) handleMonitor(_ context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	monitorCmd := cmd.(*api.MonitorCmd)

	if d.topo == nil {
		receiver.CompleteYield(tag, nil, nil)
		return nil
	}

	err := d.topo.Monitor(monitorCmd.Watcher, monitorCmd.Target)
	receiver.CompleteYield(tag, nil, err)
	return nil
}

func (d *Dispatcher) handleUnmonitor(_ context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	unmonitorCmd := cmd.(*api.UnmonitorCmd)

	if d.topo == nil {
		receiver.CompleteYield(tag, nil, nil)
		return nil
	}

	err := d.topo.Demonitor(unmonitorCmd.Watcher, unmonitorCmd.Target)
	receiver.CompleteYield(tag, nil, err)
	return nil
}

func (d *Dispatcher) handleLink(_ context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	linkCmd := cmd.(*api.LinkCmd)

	if d.topo == nil {
		receiver.CompleteYield(tag, nil, nil)
		return nil
	}

	err := d.topo.Link(linkCmd.From, linkCmd.To)
	receiver.CompleteYield(tag, nil, err)
	return nil
}

func (d *Dispatcher) handleUnlink(_ context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	unlinkCmd := cmd.(*api.UnlinkCmd)

	if d.topo == nil {
		receiver.CompleteYield(tag, nil, nil)
		return nil
	}

	err := d.topo.Unlink(unlinkCmd.From, unlinkCmd.To)
	receiver.CompleteYield(tag, nil, err)
	return nil
}

// handleExec spawns a process and waits for its result.
func (d *Dispatcher) handleExec(ctx context.Context, cmd dispatcher.Command, tag uint64, receiver dispatcher.ResultReceiver) error {
	execCmd := cmd.(*api.ExecCmd)

	// Host is required
	if execCmd.HostID == "" {
		receiver.CompleteYield(tag, nil, errors.New("host ID required for process.exec"))
		return nil
	}

	// Get required dependencies from context
	pidGen := api.GetPIDGenerator(ctx)
	if pidGen == nil {
		receiver.CompleteYield(tag, nil, errors.New("PID generator not available"))
		return nil
	}

	node := relay.GetNode(ctx)
	if node == nil {
		receiver.CompleteYield(tag, nil, errors.New("relay node not available"))
		return nil
	}

	if d.topo == nil {
		receiver.CompleteYield(tag, nil, errors.New("topology not available"))
		return nil
	}

	// Generate watcher PID for monitoring
	watcherPID := pidGen.Generate(topapi.ControlHost)

	// Register watcher in topology
	if err := d.topo.Register(watcherPID); err != nil {
		receiver.CompleteYield(tag, nil, err)
		return nil
	}

	// Attach to relay to receive exit events
	monitorCh := make(chan *relay.Package, 1)
	detach, err := node.Attach(watcherPID, monitorCh)
	if err != nil {
		d.topo.Remove(watcherPID)
		receiver.CompleteYield(tag, nil, err)
		return nil
	}

	// Prepare start options with monitoring
	options := attrs.NewBag()
	options.Set(api.ProcessParentKey, watcherPID)
	options.Set(api.ProcessMonitorKey, true)

	// Start the process
	processPID, err := d.manager.Start(ctx, &api.Start{
		HostID:  execCmd.HostID,
		Source:  execCmd.Source,
		Input:   execCmd.Input,
		Options: options,
	})
	if err != nil {
		detach()
		d.topo.Remove(watcherPID)
		receiver.CompleteYield(tag, nil, err)
		return nil
	}

	d.logger.Debug("started exec process",
		zap.String("source", execCmd.Source.String()),
		zap.String("pid", processPID.String()))

	// Wait for exit event in goroutine
	go func() {
		defer detach()
		defer d.topo.Remove(watcherPID)

		for {
			select {
			case <-ctx.Done():
				// Context canceled - cleanup and return error but don't cancel child
				receiver.CompleteYield(tag, api.ExecResult{}, ctx.Err())
				return

			case batch, ok := <-monitorCh:
				if !ok {
					receiver.CompleteYield(tag, api.ExecResult{}, errors.New("monitor channel closed"))
					return
				}

				for _, msg := range batch.Messages {
					if msg.Topic != topapi.TopicEvents {
						continue
					}
					for _, p := range msg.Payloads {
						if e, ok := p.Data().(*topapi.ExitEvent); ok {
							d.logger.Debug("received exit event",
								zap.String("source", execCmd.Source.String()),
								zap.String("pid", processPID.String()))
							receiver.CompleteYield(tag, api.ExecResult{Result: e.Result}, nil)
							return
						}
					}
				}
			}
		}
	}()

	return nil
}
