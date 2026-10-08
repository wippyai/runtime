// SPDX-License-Identifier: MPL-2.0

package actor

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/runtime"
)

type upgradeTransferProbe struct {
	next        *process.UpgradeRequest
	initErr     error
	transferErr error
	onInit      func()
	initialized bool
	transferred bool
	closed      int
}

func (p *upgradeTransferProbe) Init(context.Context, string, payload.Payloads) error {
	p.initialized = true
	if p.onInit != nil {
		p.onInit()
	}
	return p.initErr
}

func (p *upgradeTransferProbe) Step(_ []process.Event, out *process.StepOutput) error {
	if p.next != nil {
		out.SetUpgrade(p.next)
	} else {
		out.Done(nil)
	}
	return nil
}

func (p *upgradeTransferProbe) TransferUpgradeState(next process.Process) error {
	if p.closed != 0 || !next.(*upgradeTransferProbe).initialized {
		return errors.New("transfer must run after replacement init and before old close")
	}
	p.transferred = true
	return p.transferErr
}

func (p *upgradeTransferProbe) Close() { p.closed++ }

func TestUpgradeTransferClosesEachIncarnationOnce(t *testing.T) {
	failure := errors.New("upgrade failure")
	for _, name := range []string{"success", "create failure", "init failure", "transfer failure", "cancel during init"} {
		t.Run(name, func(t *testing.T) {
			completed := make(chan *runtime.Result, 1)
			sched := newTestSchedulerWithLifecycle(1, &testLifecycle{onComplete: func(_ context.Context, _ pid.PID, result *runtime.Result) {
				completed <- result
			}})
			sched.Start()
			defer testStopScheduler(sched)
			appCtx := ctxapi.WithAppContext(context.Background(), ctxapi.NewAppContext())
			ctx, cancel := context.WithCancel(appCtx)
			defer cancel()
			old := &upgradeTransferProbe{next: &process.UpgradeRequest{Source: registry.NewID("app", "next")}}
			next := &upgradeTransferProbe{}
			if name == "init failure" {
				next.initErr = failure
			}
			if name == "transfer failure" {
				old.transferErr = failure
			}
			if name == "cancel during init" {
				next.onInit = cancel
			}
			process.WithFactory(ctx, &mockFactory{createFunc: func(registry.ID) (process.Process, *process.Meta, error) {
				if name == "create failure" {
					return nil, nil, failure
				}
				return next, &process.Meta{}, nil
			}})
			_, err := sched.Submit(ctx, pid.PID{UniqID: "upgrade-transfer"}, old, "", nil)
			require.NoError(t, err)
			select {
			case result := <-completed:
				if name == "success" {
					require.NoError(t, result.Error)
				} else if name != "cancel during init" {
					require.ErrorIs(t, result.Error, failure)
				} else {
					require.Error(t, result.Error)
				}
			case <-time.After(10 * time.Second):
				t.Fatal("upgrade did not complete")
			}
			testStopScheduler(sched) // joins Close before reading the probes
			require.Equal(t, 1, old.closed)
			if name != "create failure" {
				require.Equal(t, 1, next.closed)
			}
			require.Equal(t, name != "create failure" && name != "init failure", old.transferred)
		})
	}
}
