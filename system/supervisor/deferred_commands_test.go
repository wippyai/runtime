// SPDX-License-Identifier: MPL-2.0

package supervisor

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/registry"
	apisupervisor "github.com/wippyai/runtime/api/supervisor"
	"github.com/wippyai/runtime/system/eventbus"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func TestDeferredLifecycleCommands(t *testing.T) {
	for _, ending := range []string{"commit", "discard", "replacement", "removal", "failed_commit"} {
		for _, command := range []struct {
			name string
			kind actKind
		}{{"start", actStart}, {"stop", actStop}} {
			t.Run(ending+"/"+command.name, func(t *testing.T) {
				h := newDeferredCommandHarness(t)
				h.send(action{kind: actBegin})
				h.register("target", "old", command.kind == actStop)
				h.send(action{kind: actCommit})
				h.barrier()
				before := h.calls()

				h.send(action{kind: actBegin})
				h.register("unrelated", "unrelated", false)
				if ending == "replacement" || ending == "removal" {
					h.send(action{kind: actRemove, serviceID: "target"})
				}
				if ending == "replacement" {
					h.register("target", "replacement", false)
				}
				if ending == "failed_commit" {
					h.register("bad", "bad", true)
				}
				h.send(action{kind: command.kind, serviceID: "target"})
				h.barrier()
				require.Equal(t, before, h.calls(), "command must wait for transaction closure")
				end := actCommit
				if ending == "discard" {
					end = actDiscard
				}
				h.send(action{kind: end})
				h.barrier()

				want := append([]string{}, before...)
				switch ending {
				case "replacement":
					if command.kind == actStop {
						want = append(want, "old.stop", "replacement.start")
					}
					want = append(want, "replacement."+command.name)
				case "removal":
					if command.kind == actStop {
						want = append(want, "old.stop")
					}
					failures := h.logs.FilterMessage("failed to build " + command.name + " operations").All()
					require.Len(t, failures, 1, "removed target must produce an observable failure")
					require.Equal(t, "target", failures[0].ContextMap()["serviceID"])
					require.Equal(t, NewServiceNotFoundError("target").Error(), failures[0].ContextMap()["error"])
				default:
					want = append(want, "old."+command.name)
				}
				require.Equal(t, want, h.calls())
				if ending == "failed_commit" {
					require.Equal(t, 1, h.logs.FilterMessage("failed to execute commit protocol").Len())
				}
				_, err := h.sup.GetState("unrelated")
				if ending == "discard" {
					require.Error(t, err)
				} else {
					require.NoError(t, err)
				}
			})
		}
	}
}

func TestDeferredLifecycleCommandOrder(t *testing.T) {
	for _, end := range []actKind{actCommit, actDiscard} {
		t.Run(map[actKind]string{actCommit: "commit", actDiscard: "discard"}[end], func(t *testing.T) {
			h := newDeferredCommandHarness(t)
			h.send(action{kind: actBegin})
			h.register("a", "a", false)
			h.register("b", "b", false)
			h.send(action{kind: actCommit})
			h.send(action{kind: actBegin})
			for _, next := range []action{
				{kind: actStart, serviceID: "b"},
				{kind: actStart, serviceID: "a"},
				{kind: actStop, serviceID: "b"},
				{kind: actStart, serviceID: "b"},
				{kind: actStop, serviceID: "a"},
			} {
				h.send(next)
			}
			h.barrier()
			require.Empty(t, h.calls())
			h.send(action{kind: end})
			// Later input, including the next transaction, must not overtake replay.
			h.send(action{kind: actBegin})
			h.barrier()
			require.Equal(t, []string{"b.start", "a.start", "b.stop", "b.start", "a.stop"}, h.calls())
			h.send(action{kind: actStop, serviceID: "b"})
			h.send(action{kind: actDiscard})
			h.barrier()
			require.Equal(t, []string{"b.start", "a.start", "b.stop", "b.start", "a.stop", "b.stop"}, h.calls())
		})
	}
}

type deferredCommandHarness struct {
	t     *testing.T
	sup   *Supervisor
	logs  *observer.ObservedLogs
	trace []string
	mu    sync.Mutex
}

func newDeferredCommandHarness(t *testing.T) *deferredCommandHarness {
	t.Helper()
	core, logs := observer.New(zapcore.DebugLevel)
	bus := eventbus.NewBus()
	t.Cleanup(func() { bus.Stop() })
	var rejectCommit sync.Once
	sup := NewSupervisor(bus, zap.New(core), WithDependencyResolver(func(id registry.ID) ([]registry.ID, error) {
		if id.Name == "bad" {
			var err error
			rejectCommit.Do(func() { err = errors.New("reject commit planning") })
			return nil, err
		}
		return nil, nil
	}))
	// An unbuffered channel lets a no-op receipt prove the preceding action
	// completed. No sleeps or polling decide when assertions are safe.
	sup.actions = make(chan action)
	require.NoError(t, sup.Start(context.Background()))
	t.Cleanup(func() { require.NoError(t, sup.Stop()) })
	return &deferredCommandHarness{t: t, sup: sup, logs: logs}
}

func (h *deferredCommandHarness) send(next action) {
	h.t.Helper()
	select {
	case h.sup.actions <- next:
	case <-time.After(10 * time.Second):
		h.t.Fatal("supervisor did not receive action")
	}
}

func (h *deferredCommandHarness) barrier() { h.send(action{kind: -1}) }

func (h *deferredCommandHarness) calls() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string{}, h.trace...)
}

func (h *deferredCommandHarness) register(id, instance string, autoStart bool) {
	record := func(verb string) {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.trace = append(h.trace, instance+"."+verb)
	}
	h.send(action{kind: actRegister, serviceID: id, entry: &apisupervisor.Entry{
		Service: &mockService{
			startFunc: func(context.Context) (<-chan any, error) { record("start"); return nil, nil },
			stopFunc:  func(context.Context) error { record("stop"); return nil },
		},
		Config: apisupervisor.LifecycleConfig{AutoStart: autoStart, StartTimeout: time.Second, StopTimeout: time.Second},
	}})
}
