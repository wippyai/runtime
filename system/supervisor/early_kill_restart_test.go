// SPDX-License-Identifier: MPL-2.0

package supervisor

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/security"
	supervisorapi "github.com/wippyai/runtime/api/supervisor"
	"github.com/wippyai/runtime/system/scheduler"
	"github.com/wippyai/runtime/system/scheduler/actor"
	securitysys "github.com/wippyai/runtime/system/security"
	"go.uber.org/zap"
)

type earlyKillRoot struct {
	ctx     context.Context
	entered chan struct{}
}

func (p *earlyKillRoot) Init(ctx context.Context, _ string, _ payload.Payloads) error {
	p.ctx = ctx
	return nil
}

func (p *earlyKillRoot) Step(_ []process.Event, out *process.StepOutput) error {
	close(p.entered)
	<-p.ctx.Done()
	out.Done(nil)
	return nil
}

func (*earlyKillRoot) Close() {}

type earlyKillService struct {
	scheduler *actor.Scheduler
	started   chan pid.PID
	entered   chan struct{}
	channels  map[string]chan any
	mu        sync.Mutex
	next      int
}

type earlyKillLifecycle struct{ service *earlyKillService }

func (*earlyKillLifecycle) OnStart(context.Context, pid.PID, process.Process) error { return nil }

func (l *earlyKillLifecycle) OnComplete(_ context.Context, child pid.PID, result *runtime.Result) {
	l.service.complete(child, result)
}

func (s *earlyKillService) Start(ctx context.Context) (<-chan any, error) {
	s.mu.Lock()
	s.next++
	attempt := s.next
	child := pid.PID{UniqID: fmt.Sprintf("jobs-root-%d", attempt)}
	status := make(chan any, 1)
	s.channels[child.String()] = status
	s.mu.Unlock()

	root := &earlyKillRoot{entered: make(chan struct{})}
	if _, err := s.scheduler.Submit(ctx, child, root, "", nil); err != nil {
		return nil, err
	}
	if attempt == 1 {
		go func() { <-root.entered; close(s.entered) }()
	}
	s.started <- child
	return status, nil
}

func (*earlyKillService) Stop(context.Context) error { return nil }

func (s *earlyKillService) complete(child pid.PID, result *runtime.Result) {
	s.mu.Lock()
	status := s.channels[child.String()]
	delete(s.channels, child.String())
	s.mu.Unlock()
	if status == nil {
		return
	}
	if result.Error != nil {
		status <- fmt.Errorf("process failed: %w", result.Error)
	} else {
		status <- supervisorapi.ErrExit
	}
	close(status)
}

type earlyKillPolicy struct{}

func (earlyKillPolicy) ID() registry.ID { return registry.NewID("test", "allow") }
func (earlyKillPolicy) Evaluate(security.Actor, string, string, attrs.Bag) security.Result {
	return security.Allow
}

type earlyKillSecurityRegistry struct{ security.Registry }

func (earlyKillSecurityRegistry) GetPolicyGroup(registry.ID) (security.Scope, error) {
	return securitysys.NewScope([]security.Policy{earlyKillPolicy{}}), nil
}

func TestServiceRootTerminatedDuringStepRestartsAfterCompleteDependency(t *testing.T) {
	ctx := security.WithRegistry(ctxapi.NewRootContext(), earlyKillSecurityRegistry{})
	boot := &completionService{started: make(chan struct{}), complete: make(chan error, 1), status: make(chan any)}
	bootCfg := supervisorapi.LifecycleConfig{Startup: supervisorapi.StartupComplete}
	bootCfg.InitDefaults()
	bootCtrl := NewController(ctx, boot, bootCfg, nil)
	defer bootCtrl.close()

	service := &earlyKillService{
		started: make(chan pid.PID, 2), entered: make(chan struct{}), channels: make(map[string]chan any),
	}
	lifecycle := &earlyKillLifecycle{service: service}
	service.scheduler = actor.NewScheduler(scheduler.NewRegistry(), actor.WithWorkers(2), actor.WithLifecycle(lifecycle))
	service.scheduler.Start()
	defer func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		service.scheduler.Stop(stopCtx)
	}()

	// Match the jobs worker service's dependency, auto-start, retry and process
	// security group. InitialDelay and StableThreshold use their runtime defaults.
	cfg := supervisorapi.LifecycleConfig{
		DependsOn: []string{"wippy.bootloader:bootloader.service"},
		AutoStart: true,
		RetryPolicy: supervisorapi.RetryPolicy{
			MaxDelay: 60 * time.Second, MaxAttempts: 10,
		},
		Security: &security.Config{PolicyGroups: []registry.ID{registry.NewID("wippy.security", "process")}},
	}
	cfg.InitDefaults()
	ctrl := NewController(ctx, service, cfg, nil)
	defer ctrl.close()

	done := make(chan error, 1)
	go func() {
		done <- newSequencer(zap.NewNop()).transition(ctx,
			operation{id: "wippy.bootloader:bootloader.service", controller: bootCtrl, kind: opStart},
			operation{id: "kickside.core.jobs.service:worker.service", controller: ctrl, kind: opStart,
				dependencies: cfg.RequiredServices()},
		)
	}()
	select {
	case <-boot.started:
	case <-time.After(time.Second):
		t.Fatal("boot dependency did not start")
	}
	boot.complete <- nil
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("jobs service did not start after boot completion")
	}
	first := <-service.started
	select {
	case <-service.entered:
	case <-time.After(time.Second):
		t.Fatal("jobs root did not enter its startup step")
	}
	require.NoError(t, service.scheduler.Terminate(first))
	select {
	case second := <-service.started:
		require.NotEqual(t, first, second)
	case <-time.After(4 * time.Second):
		t.Fatalf("jobs service stayed down after early termination: %+v", ctrl.State())
	}
}
