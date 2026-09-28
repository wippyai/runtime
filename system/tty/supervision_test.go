// SPDX-License-Identifier: MPL-2.0

package tty

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/runtime"
	supervisorapi "github.com/wippyai/runtime/api/supervisor"
	ttyapi "github.com/wippyai/runtime/api/tty"
	supervisorsys "github.com/wippyai/runtime/system/supervisor"
)

// This adapter uses the real supervisor controller and the same OnComplete
// barrier used by process lifecycle. Each restart receives a new process PID.
type mountSupervisionAdapter struct {
	start func() (<-chan any, error)
	stop  func()
}

func (a mountSupervisionAdapter) Start(context.Context) (<-chan any, error) { return a.start() }
func (a mountSupervisionAdapter) Stop(context.Context) error                { a.stop(); return nil }

type mountGeneration struct {
	service *Service
	ctx     context.Context
	remote  ttyapi.Viewport
	details chan any
	ref     string
	owner   pid.PID
	once    sync.Once
}

func (g *mountGeneration) finish() {
	g.once.Do(func() {
		g.service.OnComplete(g.ctx, g.owner, nil)
		close(g.details)
	})
}

func TestSupervisedMountRestartRequiresFreshAuthority(t *testing.T) {
	for _, role := range []string{"owner", "consumer"} {
		t.Run(role, func(t *testing.T) {
			a, b, _, _ := meshFixture(t)
			owners := make([]context.Context, 2)
			agents := make([]context.Context, 2)
			for i := range 2 {
				owners[i], _ = meshContext(t, a, "a", fmt.Sprintf("owner-%d", i))
				agents[i], _ = meshContext(t, b, "b", fmt.Sprintf("agent-%d", i))
			}
			fixed, err := a.Create(owners[0], 80, 24)
			require.NoError(t, err)
			var generation atomic.Int32
			var current atomic.Pointer[mountGeneration]
			started := make(chan *mountGeneration, 2)
			adapter := mountSupervisionAdapter{
				start: func() (<-chan any, error) {
					index := int(generation.Add(1)) - 1
					if index >= 2 {
						return nil, fmt.Errorf("unexpected third generation")
					}
					owner, agent, view := owners[0], agents[0], fixed
					if role == "owner" {
						owner = owners[index]
						var err error
						view, err = a.Create(owner, 80, 24)
						if err != nil {
							return nil, err
						}
					} else {
						agent = agents[index]
					}
					target, _ := runtime.GetFramePID(agent)
					ref, err := view.(ttyapi.MountableViewport).Mount(owner, target, ttyapi.MountRights{Observe: true})
					if err != nil {
						return nil, err
					}
					remote, err := b.Attach(agent, ref)
					if err != nil {
						return nil, err
					}
					g := &mountGeneration{service: b, ctx: agent, owner: target, ref: ref, remote: remote, details: make(chan any)}
					if role == "owner" {
						g.service = a
						g.ctx = owner
						g.owner, _ = runtime.GetFramePID(owner)
					}
					current.Store(g)
					started <- g
					return g.details, nil
				},
				stop: func() {
					if g := current.Load(); g != nil {
						g.finish()
					}
				},
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			controller := supervisorsys.NewController(ctx, adapter, supervisorapi.LifecycleConfig{
				StartTimeout: time.Second, StopTimeout: time.Second, StableThreshold: time.Hour,
				RetryPolicy: supervisorapi.RetryPolicy{MaxAttempts: 3, InitialDelay: time.Millisecond, MaxDelay: time.Millisecond},
			}, nil)
			require.NoError(t, controller.Start())
			defer controller.Stop()
			first := <-started
			first.finish() // Unexpected exit closes the status stream and triggers retry.
			var second *mountGeneration
			select {
			case second = <-started:
			case <-time.After(2 * time.Second):
				t.Fatal("supervisor did not restart")
			}
			require.NotEqual(t, first.ref, second.ref)
			require.Eventually(t, func() bool { return first.remote.Snapshot().Width == 0 }, time.Second, time.Millisecond)
			require.Equal(t, 80, second.remote.Snapshot().Width)
			agent := agents[0]
			if role == "consumer" {
				agent = agents[1]
			}
			_, err = b.Attach(agent, first.ref)
			require.Error(t, err, "restart must not revive a previous mount")
			require.NoError(t, controller.Stop())
			require.Eventually(t, func() bool {
				a.mu.Lock()
				mounts := len(a.mounts)
				a.mu.Unlock()
				b.mesh.mu.Lock()
				views := len(b.mesh.views)
				b.mesh.mu.Unlock()
				return mounts == 0 && views == 0
			}, time.Second, time.Millisecond, "supervised stop must release every generation's mounts")
		})
	}
}
