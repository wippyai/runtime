// SPDX-License-Identifier: MPL-2.0

package jet

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/resource"
	"github.com/wippyai/runtime/api/service/template"
	"github.com/wippyai/runtime/system/eventbus"
	payloadsystem "github.com/wippyai/runtime/system/payload"
	payloadjson "github.com/wippyai/runtime/system/payload/json"
	registrysystem "github.com/wippyai/runtime/system/registry"
	"github.com/wippyai/runtime/system/registry/events"
	historymem "github.com/wippyai/runtime/system/registry/history/memory"
	"github.com/wippyai/runtime/system/registry/runner"
	"github.com/wippyai/runtime/system/registry/topology"
	resourcesystem "github.com/wippyai/runtime/system/resource"
	"go.uber.org/zap"
)

type resourceDeliveryBus struct {
	*eventbus.Bus
	deliver func(context.Context, event.Event)
}

func (b *resourceDeliveryBus) Send(ctx context.Context, e event.Event) {
	if e.System == resource.System && b.deliver != nil {
		b.deliver(ctx, e)
		return
	}
	b.Bus.Send(ctx, e)
}

func newApplyFixture(t testing.TB, overlay bool, timeout time.Duration) (
	*resourceDeliveryBus, *resourcesystem.Registry, func(...registry.Operation) error,
) {
	t.Helper()
	bus := &resourceDeliveryBus{Bus: eventbus.NewBus()}
	ctx := event.WithBus(ctxapi.NewRootContext(), bus)
	await := eventbus.NewAwaitService(bus)
	require.NoError(t, await.Start(ctx))
	ctx = event.WithAwaitService(ctx, await)
	router, err := eventbus.StartRouter(ctx, bus)
	require.NoError(t, err)
	resources := resourcesystem.NewRegistry(bus, nil)
	require.NoError(t, resources.Start(ctx))
	t.Cleanup(func() {
		bus.Stop()
		require.NoError(t, router.Stop())
		require.NoError(t, resources.Stop())
		await.Stop()
	})

	transcoder := payloadsystem.NewTranscoder()
	payloadjson.Register(transcoder)
	manager := NewManager(bus, transcoder, nil)
	require.NoError(t, router.AddHandler(events.NewRegistryHandler("template.(jet|set)", manager)))
	logger := zap.NewNop()
	resolver := topology.NewResolver()
	builder := topology.NewStateBuilder(logger, resolver)
	reg := registrysystem.NewRegistry(historymem.New(), runner.NewBusRunner(bus, logger, builder,
		runner.WithEventWaitTimeout(timeout)), builder, resolver, logger)

	var generation uint64
	return bus, resources, func(changes ...registry.Operation) error {
		if !overlay {
			_, err := reg.Apply(ctx, changes)
			return err
		}
		next, err := reg.ApplyOverlay(ctx, "test:owner", generation, changes)
		if err == nil {
			generation = next
		}
		return err
	}
}

func TestApplyTemplateSetReadiness(t *testing.T) {
	for _, mode := range []string{"durable", "overlay"} {
		t.Run(mode, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				bus, resources, apply := newApplyFixture(t, mode == "overlay", 0)
				id := registry.NewID("test", "views")
				entry := makeSetEntry(id, &template.SetConfig{})
				updated := makeSetEntry(id, &template.SetConfig{Engine: template.EngineConfig{
					Delimiters: template.DelimiterConfig{Left: "[[", Right: "]]"},
				}})
				tpl := makeTemplateEntry(registry.NewID("test", "greeting"), &template.Config{
					Set: id, Source: `Hello [[ name ]]`,
				})
				const delay = 10 * time.Millisecond
				bus.deliver = func(ctx context.Context, e event.Event) {
					switch e.Kind {
					case resource.Register, resource.Update, resource.Delete:
						bus.Bus.Send(ctx, event.Event{System: resource.System, Kind: resource.Accept, Path: id.String()})
						go func() {
							time.Sleep(delay)
							bus.Bus.Send(ctx, e)
						}()
					default:
						bus.Bus.Send(ctx, e)
					}
				}
				acquire := func() *Set {
					res, err := resources.Acquire(context.Background(), id, resource.ModeNormal)
					if !assert.NoError(t, err, "acquire immediately after apply") {
						return nil
					}
					t.Cleanup(res.Release)
					value, err := res.Get()
					require.NoError(t, err)
					return value.(*Set)
				}

				require.NoError(t, apply(registry.Operation{Kind: registry.EntryCreate, Entry: entry}))
				acquire()
				time.Sleep(delay)
				synctest.Wait()

				require.NoError(t, apply(registry.Operation{Kind: registry.EntryCreate, Entry: tpl}))
				require.NoError(t, apply(registry.Operation{Kind: registry.EntryUpdate, Entry: updated}))
				if set := acquire(); set != nil {
					got, err := set.RenderTemplate("greeting", map[string]any{"name": "World"})
					require.NoError(t, err)
					assert.Equal(t, "Hello World", got, "render immediately after update")
				}
				time.Sleep(delay)
				synctest.Wait()

				require.NoError(t, apply(registry.Operation{Kind: registry.EntryDelete, Entry: tpl}))
				require.NoError(t, apply(registry.Operation{Kind: registry.EntryDelete, Entry: updated}))
				res, err := resources.Acquire(context.Background(), id, resource.ModeNormal)
				if res != nil {
					res.Release()
				}
				assert.ErrorIs(t, err, resource.ErrNotFound, "acquire immediately after delete")

				require.NoError(t, apply(registry.Operation{Kind: registry.EntryCreate, Entry: entry}))
				if set := acquire(); set != nil {
					assert.Equal(t, "{{", set.Config().Engine.Delimiters.Left, "acquire the recreated set")
				}
				time.Sleep(delay)
				synctest.Wait()
			})
		})
	}
}

func TestApplyTemplateSetResourceFailure(t *testing.T) {
	for _, mode := range []string{"durable", "overlay"} {
		t.Run(mode, func(t *testing.T) {
			for _, failure := range []string{"rejected", "timeout"} {
				t.Run(failure, func(t *testing.T) {
					synctest.Test(t, func(t *testing.T) {
						bus, resources, apply := newApplyFixture(t, mode == "overlay", time.Second)
						bus.deliver = func(ctx context.Context, e event.Event) {
							if e.Kind == resource.Register {
								if failure == "timeout" {
									return
								}
								entry := e.Data.(resource.Entry)
								entry.Provider = nil
								e.Data = entry
							}
							bus.Bus.Send(ctx, e)
						}
						id := registry.NewID("test", "views")
						err := apply(registry.Operation{Kind: registry.EntryCreate, Entry: makeSetEntry(id, &template.SetConfig{})})
						if failure == "timeout" {
							require.ErrorContains(t, err, "timeout")
						} else {
							require.ErrorContains(t, err, "resource entry has nil provider")
						}
						_, err = resources.Acquire(context.Background(), id, resource.ModeNormal)
						require.ErrorIs(t, err, resource.ErrNotFound)
					})
				})
			}
		})
	}
}
