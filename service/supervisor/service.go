// SPDX-License-Identifier: MPL-2.0

// Package supervisor provides registry-driven process supervision.
package supervisor

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/wippyai/runtime/api/attrs"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	processapi "github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	supervisorapi "github.com/wippyai/runtime/api/service/supervisor"
	"github.com/wippyai/runtime/api/supervisor"
	topologyapi "github.com/wippyai/runtime/api/topology"
	bootpkg "github.com/wippyai/runtime/boot"
)

// Service represents a running process service instance managed by supervisor.
// It monitors a child process via topology and reports status changes.
type Service struct {
	pidGen            processapi.PIDGenerator
	statusCh          chan any
	detachFn          context.CancelFunc
	gate              *bootpkg.Gate
	completion        *bootpkg.Gate
	restartCompletion *bootpkg.Gate
	restartRequested  *atomic.Bool
	supervisorPID     pid.PID
	childPID          pid.PID
	id                registry.ID
	config            supervisorapi.ServiceConfig
	completionMu      sync.RWMutex
}

// NewService creates a new process service instance.
func NewService(id registry.ID, config supervisorapi.ServiceConfig, pidGen processapi.PIDGenerator) *Service {
	return &Service{
		id:               id,
		config:           config,
		pidGen:           pidGen,
		restartRequested: &atomic.Bool{},
	}
}

// SetGate attaches a boot readiness gate to the service.
func (svc *Service) SetGate(gate *bootpkg.Gate) {
	svc.gate = gate
}

// WaitCompletion makes a startup: complete service a real dependency barrier.
func (svc *Service) WaitCompletion(ctx context.Context) error {
	svc.completionMu.RLock()
	completion := svc.completion
	svc.completionMu.RUnlock()
	if completion == nil {
		return ErrNoCompletionGate
	}
	return completion.Wait(ctx)
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

	svc.completionMu.Lock()
	completion := svc.restartCompletion
	svc.restartCompletion = nil
	if completion == nil && (svc.config.Lifecycle.Startup == supervisor.StartupComplete || svc.gate != nil) {
		completion = bootpkg.NewReadiness().RegisterGate(svc.id.String())
	}
	svc.completion = completion
	svc.completionMu.Unlock()

	// Generate supervisor PID for monitoring (using control host)
	svc.supervisorPID = svc.pidGen.Generate(topologyapi.ControlHost)

	// Register supervisor in topology FIRST (before starting child)
	if err := topo.Register(svc.supervisorPID); err != nil {
		completion.Fail(err)
		return nil, newRegisterPIDError(err)
	}

	// Attach to relay to receive exit events
	monitorCh := make(chan *relay.Package, 1)
	detach, err := node.Attach(svc.supervisorPID, monitorCh)
	if err != nil {
		topo.Remove(svc.supervisorPID)
		completion.Fail(err)
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

	svc.statusCh = make(chan any, 1)
	restartRequested := &atomic.Bool{}
	svc.restartRequested = restartRequested

	// Start the child process
	// lifecycle.OnStart will atomically:
	// - Register child PID in topology
	// - Call topology.Wait(supervisorPID, childPID)
	childPID, err := manager.Start(ctx, &processapi.Start{
		HostID:  svc.config.HostID,
		Source:  svc.config.Process,
		Input:   payloads,
		Options: opts,
		Context: []ctxapi.Pair{{Key: processapi.OutdatedSupervisorKey, Value: svc.supervisorPID}},
	})
	if err != nil {
		detach()
		topo.Remove(svc.supervisorPID)
		completion.Fail(err)
		if svc.gate != nil {
			svc.gate.Fail(err)
		}
		return nil, newStartProcessError(err)
	}

	svc.childPID = childPID
	// Start monitor goroutine
	go svc.monitorLoop(ctx, monitorCh, completion)

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
	case status, open := <-svc.statusCh:
		if _, restart := status.(supervisor.Restart); !open || !restart {
			return nil
		}
	default:
	}

	if svc.gate != nil && !svc.restartRequested.Load() {
		svc.gate.Fail(fmt.Errorf("service stopped before completion"))
	}
	svc.completionMu.RLock()
	completion := svc.completion
	svc.completionMu.RUnlock()
	if !svc.restartRequested.Load() {
		completion.Fail(fmt.Errorf("service stopped before completion"))
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
	for {
		select {
		case status, open := <-svc.statusCh:
			if _, restart := status.(supervisor.Restart); !open || !restart {
				return nil
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// monitorLoop listens for topology exit events and reports them via status channel.
func (svc *Service) monitorLoop(ctx context.Context, ch <-chan *relay.Package, completion *bootpkg.Gate) {
	statusCh, detach, restartRequested := svc.statusCh, svc.detachFn, svc.restartRequested
	defer close(statusCh)
	if detach != nil {
		defer detach()
	}

	for {
		select {
		case <-ctx.Done():
			completion.Fail(ctx.Err())
			if svc.gate != nil {
				svc.gate.Fail(ctx.Err())
			}
			return

		case pkg, ok := <-ch:
			if !ok {
				completion.Fail(fmt.Errorf("relay monitor channel closed"))
				if svc.gate != nil {
					svc.gate.Fail(fmt.Errorf("relay monitor channel closed"))
				}
				select {
				case statusCh <- supervisor.ErrExit:
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

					if event.Kind == topologyapi.OutdatedRejected {
						if event.From != svc.childPID {
							continue
						}
						if restartRequested.CompareAndSwap(false, true) {
							svc.completionMu.Lock()
							svc.restartCompletion = completion
							svc.completionMu.Unlock()
							statusCh <- supervisor.Restart{Graceful: true}
						}
						continue
					}

					if event.Kind == topologyapi.Exit && event.Result != nil && event.Result.Error == nil {
						if event.Result.Outdated && !restartRequested.Load() {
							svc.completionMu.Lock()
							svc.restartCompletion = completion
							svc.completionMu.Unlock()
							statusCh <- supervisor.Restart{}
							return
						}
						if !restartRequested.Load() {
							completion.Ready()
						}
						if svc.gate != nil && !restartRequested.Load() {
							svc.gate.Ready()
						}
						select {
						case statusCh <- supervisor.ErrExit:
						default:
						}
					} else {
						var gateErr error
						if event.Result != nil && event.Result.Error != nil {
							gateErr = event.Result.Error
						} else {
							gateErr = errors.New("process exited without return result")
						}
						completion.Fail(gateErr)
						if svc.gate != nil {
							svc.gate.Fail(gateErr)
						}
						if event.Result != nil && event.Result.Error != nil {
							select {
							case statusCh <- fmt.Errorf("process failed: %w", event.Result.Error):
							default:
							}
						} else {
							select {
							case statusCh <- supervisor.ErrExit:
							default:
							}
						}
					}
					return
				}
			}
		}
	}
}

var _ supervisor.Service = (*Service)(nil)
