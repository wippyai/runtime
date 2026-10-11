// SPDX-License-Identifier: MPL-2.0

package sql

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	sqlapi "github.com/wippyai/runtime/api/service/sql"
	"github.com/wippyai/runtime/system/registry/events"
)

type readinessBus struct {
	subs            map[string]chan<- event.Event
	requests        chan event.Event
	registryReplies chan event.Event
	mu              sync.Mutex
}

func (b *readinessBus) Subscribe(ctx context.Context, system string, ch chan<- event.Event) (string, error) {
	return b.SubscribeP(ctx, system, "*", ch)
}
func (b *readinessBus) SubscribeP(_ context.Context, _ string, _ string, ch chan<- event.Event) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	id := fmt.Sprint(len(b.subs) + 1)
	b.subs[id] = ch
	return id, nil
}
func (b *readinessBus) Unsubscribe(_ context.Context, id string) {
	b.mu.Lock()
	delete(b.subs, id)
	b.mu.Unlock()
}
func (b *readinessBus) HasSubscribers(_, _ string) bool { return true }
func (b *readinessBus) Send(_ context.Context, e event.Event) {
	if e.System == resource.System && (e.Kind == resource.Register || e.Kind == resource.Delete) {
		b.requests <- e
	}
	if e.System == registry.System {
		b.registryReplies <- e
	}
}
func (b *readinessBus) reply(e event.Event) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, ch := range b.subs {
		ch <- e
	}
}

func TestRegistrySQLWaitsForResourceOutcome(t *testing.T) {
	for _, deletion := range []bool{false, true} {
		for _, rejected := range []bool{false, true} {
			t.Run(fmt.Sprintf("delete=%v/rejected=%v", deletion, rejected), func(t *testing.T) {
				synctest.Test(t, func(t *testing.T) {
					bus := &readinessBus{subs: make(map[string]chan<- event.Event), requests: make(chan event.Event, 1), registryReplies: make(chan event.Event, 1)}
					manager, err := NewManagerWithFactory(&TestTranscoder{}, bus, nil, nil, NewTestPoolFactory())
					require.NoError(t, err)
					entry := registry.Entry{ID: registry.ID{NS: "test", Name: "db"}, Kind: sqlapi.SQLite, Data: payload.New(map[string]any{})}
					if deletion {
						p := NewMockConnPool(sqlapi.SQLite)
						manager.services[entry.ID] = p
						defer p.Stop(context.Background())
					}
					defer func() {
						for _, p := range manager.services {
							_ = p.Stop(context.Background())
						}
					}()
					ctx := event.WithBus(ctxapi.NewRootContext(), bus)
					kind := registry.EntryCreate
					if deletion {
						kind = registry.EntryDelete
					}
					finished := make(chan struct{})
					go func() {
						_ = events.NewRegistryHandler(entry.Kind, manager).Handle(ctx, event.Event{System: registry.System, Kind: kind, Data: entry})
						close(finished)
					}()
					synctest.Wait()
					select {
					case <-finished:
						t.Fatal("registry accepted SQL change before resource acknowledgement")
					default:
					}
					request := <-bus.requests
					replyPath := request.Path
					if e, ok := request.Data.(resource.Entry); ok {
						replyPath = e.OpID
					}
					require.NotEmpty(t, replyPath)
					bus.reply(event.Event{System: resource.System, Kind: resource.Accept, Path: "unrelated-operation"})
					synctest.Wait()
					select {
					case <-finished:
						t.Fatal("unrelated acknowledgement completed SQL change")
					default:
					}
					outcome := resource.Accept
					if rejected {
						outcome = resource.Reject
					}
					bus.reply(event.Event{System: resource.System, Kind: outcome, Path: replyPath, Data: "resource refused"})
					<-finished
					reply := <-bus.registryReplies
					expected := registry.EntryAccept
					if rejected {
						expected = registry.EntryReject
						require.ErrorContains(t, reply.Data.(error), "resource refused")
					}
					require.Equal(t, expected, reply.Kind)
					_, exists := manager.services[entry.ID]
					require.Equal(t, (!deletion && !rejected) || (deletion && rejected), exists)
				})
			})
		}
	}
}
