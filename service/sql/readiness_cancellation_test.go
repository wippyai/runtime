// SPDX-License-Identifier: MPL-2.0

package sql

import (
	"context"
	"sync/atomic"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	sqlapi "github.com/wippyai/runtime/api/service/sql"
	"github.com/wippyai/runtime/api/supervisor"
	"github.com/wippyai/runtime/system/eventbus"
	systemresource "github.com/wippyai/runtime/system/resource"
)

type heldResourceOutcomeBus struct {
	event.Bus
	held chan event.Event
	hold atomic.Bool
}

func (b *heldResourceOutcomeBus) Send(ctx context.Context, e event.Event) {
	if b.hold.Load() && e.System == resource.System && e.Kind == resource.Accept {
		b.held <- e
		return
	}
	b.Bus.Send(ctx, e)
}

func TestSQLResourceCancellationAfterDispatchCompletesHandoff(t *testing.T) {
	for _, deletion := range []bool{false, true} {
		name := "register"
		if deletion {
			name = "delete"
		}
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				underlying := eventbus.NewBus()
				defer underlying.Stop()
				bus := &heldResourceOutcomeBus{Bus: underlying, held: make(chan event.Event, 1)}
				resources := systemresource.NewRegistry(bus, nil)
				rootCtx := ctxapi.NewRootContext()
				require.NoError(t, resources.Start(rootCtx))
				defer resources.Stop()
				lifecycle := make(chan event.Event, 4)
				sub, err := eventbus.NewSubscriber(rootCtx, bus, supervisor.System, "*", func(e event.Event) { lifecycle <- e })
				require.NoError(t, err)
				defer sub.Close()
				manager, err := NewManagerWithFactory(&TestTranscoder{}, bus, nil, nil, NewTestPoolFactory())
				require.NoError(t, err)
				entry := registry.Entry{ID: registry.NewID("test", "db"), Kind: sqlapi.SQLite, Data: payload.New(map[string]any{})}
				if deletion {
					require.NoError(t, manager.Add(rootCtx, entry))
					defer manager.services[entry.ID].Stop(context.Background())
					<-lifecycle
				}
				bus.hold.Store(true)
				ctx, cancel := context.WithCancel(rootCtx)
				defer cancel()
				done := make(chan error, 1)
				go func() {
					if deletion {
						done <- manager.Delete(ctx, entry)
					} else {
						done <- manager.Add(ctx, entry)
					}
				}()
				ack := <-bus.held
				require.Equal(t, !deletion, resources.Exists(entry.ID), "routing changes before its acknowledgement")
				cancel()
				synctest.Wait()
				select {
				case err := <-done:
					t.Fatalf("abandoned an applied resource operation: %v", err)
				default:
				}
				bus.hold.Store(false)
				bus.Bus.Send(rootCtx, ack)
				require.NoError(t, <-done)
				_, retained := manager.services[entry.ID]
				require.Equal(t, !deletion, retained)
				expected := supervisor.ServiceRegister
				if deletion {
					expected = supervisor.ServiceRemove
				} else {
					defer manager.services[entry.ID].Stop(context.Background())
				}
				require.Equal(t, expected, (<-lifecycle).Kind, "lifecycle delivery must survive caller cancellation")
				if deletion {
					// A new generation with the same ID remains installable.
					require.NoError(t, manager.Add(rootCtx, entry))
					defer manager.services[entry.ID].Stop(context.Background())
				}
			})
		})
	}
}
