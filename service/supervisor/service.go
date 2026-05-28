// SPDX-License-Identifier: MPL-2.0

// Package supervisor provides registry-driven process supervision.
package supervisor

import (
	"context"
	"fmt"

	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	processapi "github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	supervisorapi "github.com/wippyai/runtime/api/service/supervisor"
	"github.com/wippyai/runtime/api/supervisor"
	topologyapi "github.com/wippyai/runtime/api/topology"
)

// Service represents a running process service instance managed by supervisor.
// It monitors a child process via topology and reports status changes.
type Service struct {
	pidGen        processapi.PIDGenerator
	statusCh      chan any
	detachFn      context.CancelFunc
	supervisorPID pid.PID
	childPID      pid.PID
	id            registry.ID
	config        supervisorapi.ServiceConfig
}

// NewService creates a new process service instance.
func NewService(id registry.ID, config supervisorapi.ServiceConfig, pidGen processapi.PIDGenerator) *Service {
	return &Service{
		id:     id,
		config: config,
		pidGen: pidGen,
	}
}

// Start initiates the supervised process and begins monitoring.
// The flow is TOCTOU-safe:
// 1. Register supervisor PID in topology
// 2. Attach to relay for events
// 3. Start child process with monitoring enabled
// 4. Child registration + Wait() happens atomically in lifecycle
func (svc *Service) Start(ctx context.Context) (<-chan any, error) {
	node := relay.GetNode(ctx)
	if node == nil {
		return nil, ErrNoRelayNode
	}

	topo := topologyapi.GetTopology(ctx)
	if topo == nil {
		return nil, ErrNoTopology
	}

	manager := processapi.GetManager(ctx)
	if manager == nil {
		return nil, ErrNoProcessManager
	}

	// Generate supervisor PID for monitoring (using control host)
	svc.supervisorPID = svc.pidGen.Generate(topologyapi.ControlHost)

	// Register supervisor in topology FIRST (before starting child)
	if err := topo.Register(svc.supervisorPID); err != nil {
		return nil, newRegisterPIDError(err)
	}

	// Attach to relay to receive exit events
	monitorCh := make(chan *relay.Package, 1)
	detach, err := node.Attach(svc.supervisorPID, monitorCh)
	if err != nil {
		topo.Remove(svc.supervisorPID)
		return nil, newAttachRelayError(err)
	}
	svc.detachFn = detach

	// Prepare process start options with monitoring
	opts := attrs.NewBag()
	opts.Set(processapi.ProcessParentKey, svc.supervisorPID)
	opts.Set(processapi.ProcessMonitorKey, true)

	// Prepare input payloads
	var payloads payload.Payloads
	for _, p := range svc.config.Input {
		payloads = append(payloads, payload.New(p))
	}

	// Start the child process
	// lifecycle.OnStart will atomically:
	// - Register child PID in topology
	// - Call topology.Wait(supervisorPID, childPID)
	childPID, err := manager.Start(ctx, &processapi.Start{
		HostID:  svc.config.HostID,
		Source:  svc.config.Process,
		Input:   payloads,
		Options: opts,
	})
	if err != nil {
		detach()
		topo.Remove(svc.supervisorPID)
		return nil, newStartProcessError(err)
	}

	svc.childPID = childPID
	svc.statusCh = make(chan any, 1)

	// Start monitor goroutine
	go svc.monitorLoop(ctx, monitorCh)

	return svc.statusCh, nil
}

// Stop terminates the supervised process gracefully.
func (svc *Service) Stop(ctx context.Context) error {
	// Not started or already stopped
	if svc.statusCh == nil {
		return nil
	}

	// The child may have already exited and reported its final status before
	// shutdown reaches this service. In that state there is nothing left to
	// cancel; sending a cancel package can produce a misleading "process not
	// found" error even though the service is already stopped.
	select {
	case <-svc.statusCh:
		return nil
	default:
	}

	node := relay.GetNode(ctx)
	if node == nil {
		return ErrNoRelayNode
	}

	cancelPkg := topologyapi.CancelPackage(svc.supervisorPID, svc.childPID, "service stopping")
	if err := node.Send(cancelPkg); err != nil {
		return newSendCancelError(err)
	}

	// Wait for status channel to close (indicating process exit)
	select {
	case <-svc.statusCh:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// monitorLoop listens for topology exit events and reports them via status channel.
func (svc *Service) monitorLoop(ctx context.Context, ch <-chan *relay.Package) {
	defer close(svc.statusCh)
	defer func() {
		if svc.detachFn != nil {
			svc.detachFn()
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return

		case pkg, ok := <-ch:
			if !ok {
				select {
				case svc.statusCh <- supervisor.ErrExit:
				default:
				}
				return
			}

			for _, msg := range pkg.Messages {
				if msg.Topic != topologyapi.TopicEvents {
					continue
				}
				for _, p := range msg.Payloads {
					event, ok := p.Data().(*topologyapi.ExitEvent)
					if !ok {
						continue
					}

					if event.Result != nil && event.Result.Error != nil {
						select {
						case svc.statusCh <- fmt.Errorf("process failed: %w", event.Result.Error):
						default:
						}
					} else {
						select {
						case svc.statusCh <- supervisor.ErrExit:
						default:
						}
					}
					return
				}
			}
		}
	}
}

var _ supervisor.Service = (*Service)(nil)
