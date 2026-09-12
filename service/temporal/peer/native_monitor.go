// SPDX-License-Identifier: MPL-2.0
package peer

import (
	"context"
	"fmt"

	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/runtime"
	temporalapi "github.com/wippyai/runtime/api/service/temporal"
	"github.com/wippyai/runtime/api/topology"
	systopology "github.com/wippyai/runtime/system/topology"
	"go.uber.org/zap"
)

type ReceiverOption func(*Receiver)

func WithMonitorConfig(cfg temporalapi.MonitorConfig) ReceiverOption {
	return func(r *Receiver) { cfg.InitDefaults(); r.monitorConfig = cfg }
}

type nativeObserver struct {
	result  *runtime.Result
	request topology.NativeMonitor
	notify  topology.MonitorCompletion
}

func (r *Receiver) validateNativeMonitor(ctx context.Context, request topology.NativeMonitor) error {
	if ctx == nil {
		return fmt.Errorf("native monitor requires context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := r.monitorConfig.Validate(); err != nil {
		return err
	}
	if request.Target.Node != r.nodeID || request.Target.Host == "" || request.Target.UniqID == "" || request.Caller.Node == "" || request.Caller.Host == "" || request.Reference == "" || len(request.Reference) > 64 {
		return fmt.Errorf("invalid native Temporal monitor identity")
	}
	return nil
}

func (r *Receiver) AdmitMonitor(ctx context.Context, request topology.NativeMonitor, notify topology.MonitorCompletion) error {
	if err := r.validateNativeMonitor(ctx, request); err != nil {
		return err
	}
	if notify == nil {
		return fmt.Errorf("native monitor requires completion callback")
	}
	r.mu.Lock()
	if r.stopped || r.ctx.Err() != nil {
		r.mu.Unlock()
		return context.Canceled
	}
	if r.client == nil {
		r.mu.Unlock()
		return fmt.Errorf("Temporal client unavailable")
	}
	r.calls.Add(1)
	defer r.calls.Done()
	watcher := r.watchers[request.Target.UniqID]
	if watcher == nil {
		if len(r.watchers) >= r.monitorConfig.MaxTargets {
			r.mu.Unlock()
			return fmt.Errorf("Temporal monitor target capacity exhausted")
		}
		watcher = &workflowWatcher{workflowID: request.Target.UniqID, taskQueue: request.Target.Host, monitors: make(map[string]pid.PID), links: make(map[string]pid.PID)}
		r.watchers[watcher.workflowID] = watcher
	}
	if watcher.taskQueue != request.Target.Host {
		r.mu.Unlock()
		return fmt.Errorf("Temporal monitor task queue mismatch")
	}
	key := request.Caller.String()
	observer := nativeObserver{request: request, notify: notify}
	if existing, ok := watcher.nativeMonitors[key]; ok && existing.request.Reference == request.Reference && existing.result != nil {
		observer.result = existing.result
		watcher.nativeMonitors[key] = observer
		r.mu.Unlock()
		return r.deliverNativeResult(ctx, watcher, observer, observer.result)
	}

	if watcher.nativeRefs == nil {
		refs, err := systopology.NewMonitorReferences(r.monitorConfig.MaxObservers)
		if err != nil {
			r.mu.Unlock()
			return err
		}
		watcher.nativeRefs = refs
		watcher.nativeMonitors = make(map[string]nativeObserver)
	}
	if err := watcher.nativeRefs.Admit(request.Caller, request.Previous, request.Reference); err != nil {
		r.mu.Unlock()
		return err
	}
	watcher.nativeMonitors[key] = observer
	if watcher.nativeResult != nil {
		// A new relationship observes the next/current logical workflow run.
		// Old pending results remain attached to their original references.
		watcher.nativeResult = nil
		watcher.runID = ""
	}
	r.assignRunIDIfAvailable(watcher)
	if !watcher.watching {
		r.startWorkflowWatchLocked(watcher)
	}
	r.mu.Unlock()
	return nil
}

func (r *Receiver) ReleaseMonitor(ctx context.Context, request topology.NativeMonitor) error {
	if err := r.validateNativeMonitor(ctx, request); err != nil {
		return err
	}
	if request.Previous != "" {
		return fmt.Errorf("native monitor release cannot name a predecessor")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.stopped || r.ctx.Err() != nil {
		return context.Canceled
	}
	watcher := r.watchers[request.Target.UniqID]
	if watcher == nil || watcher.nativeRefs == nil {
		return topology.ErrPIDNotRegistered
	}
	if watcher.taskQueue != request.Target.Host {
		return fmt.Errorf("Temporal monitor task queue mismatch")
	}
	key := request.Caller.String()
	if err := watcher.nativeRefs.Release(request.Caller, request.Reference); err != nil {
		return err
	}
	delete(watcher.nativeMonitors, key)
	r.cleanupWatcherIfEmpty(watcher)
	return nil
}

func (r *Receiver) startWorkflowWatchLocked(watcher *workflowWatcher) {
	watcher.watching = true
	attempt := new(workflowObservation)
	watcher.observation = attempt
	ctx, cancel := context.WithCancel(r.ctx)
	watcher.cancel = cancel
	r.observers.Add(1)
	go func() { defer r.observers.Done(); r.watchWorkflowAttempt(ctx, watcher, attempt) }()
}

func (r *Receiver) deliverNativeResult(ctx context.Context, watcher *workflowWatcher, observer nativeObserver, result *runtime.Result) error {
	bounded, cancel := context.WithTimeout(ctx, r.monitorConfig.DeliveryTimeout)
	stop := context.AfterFunc(r.ctx, cancel)
	defer stop()
	defer cancel()
	if err := observer.notify(bounded, result); err != nil {
		r.log.Debug("native workflow completion retained for retry", zap.String("workflow_id", watcher.workflowID), zap.Error(err))
		return err
	}
	r.mu.Lock()
	if current := r.watchers[watcher.workflowID]; current == watcher {
		key := observer.request.Caller.String()
		if active, ok := current.nativeMonitors[key]; ok && active.request.Reference == observer.request.Reference {
			delete(current.nativeMonitors, key)
		}
	}
	r.mu.Unlock()
	return nil
}

var _ topology.NativeMonitorProvider = (*Receiver)(nil)
