// SPDX-License-Identifier: MPL-2.0

package lua

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/wippyai/runtime/api/boot"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/dispatcher"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/event"
	logapi "github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/registry"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	bootpkg "github.com/wippyai/runtime/boot"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

type recordingDispatcher struct{}

func (recordingDispatcher) Get(dispatcher.CommandID) dispatcher.Handler { return nil }
func (recordingDispatcher) Has(dispatcher.CommandID) bool               { return true }
func (recordingDispatcher) Dispatch(dispatcher.Command) dispatcher.Handler {
	return nil
}

type recordingBus struct {
	events []event.Event
}

func (*recordingBus) Subscribe(context.Context, event.System, chan<- event.Event) (event.SubscriberID, error) {
	return "", nil
}
func (*recordingBus) SubscribeP(context.Context, event.System, event.Kind, chan<- event.Event) (event.SubscriberID, error) {
	return "", nil
}
func (*recordingBus) Unsubscribe(context.Context, event.SubscriberID) {}
func (b *recordingBus) Send(_ context.Context, evt event.Event) {
	b.events = append(b.events, evt)
}

func engineTestContext(t *testing.T, logger *zap.Logger) (context.Context, *recordingBus) {
	t.Helper()
	ctx := ctxapi.NewRootContext()
	bus := &recordingBus{}
	ctx = event.WithBus(ctx, bus)
	ctx = logapi.WithLogger(ctx, logger)
	ctx = bootpkg.WithHandlerRegistry(ctx, bootpkg.NewHandlerRegistry())
	if err := dispatcher.WithRegistry(ctx, recordingDispatcher{}); err != nil {
		t.Fatalf("dispatcher.WithRegistry() error = %v", err)
	}
	return ctx, bus
}

func TestL01AllComponentGraphInvariant(t *testing.T) {
	seen := make(map[string]struct{})
	engineCount := 0
	for index, component := range All() {
		if component == nil {
			t.Fatalf("All()[%d] is nil", index)
		}
		name := component.Name()
		if name == "" {
			t.Fatalf("All()[%d] has an empty name", index)
		}
		if _, duplicate := seen[name]; duplicate {
			t.Fatalf("All() contains duplicate component name %q", name)
		}
		seen[name] = struct{}{}
		if name == "lua.engine" {
			engineCount++
		}
	}
	if engineCount != 1 {
		t.Fatalf("All() engine count = %d, want 1", engineCount)
	}
}

func TestL02EngineLoadRequiresDispatcher(t *testing.T) {
	ctx := ctxapi.NewRootContext()
	ctx = event.WithBus(ctx, &recordingBus{})
	ctx = bootpkg.WithHandlerRegistry(ctx, bootpkg.NewHandlerRegistry())

	loaded, err := Engine().Load(ctx)
	if !errors.Is(err, ErrDispatcherNotFound) {
		t.Fatalf("Load() error = %v, want %v", err, ErrDispatcherNotFound)
	}
	if GetCodeManager(loaded) != nil {
		t.Fatal("Load() attached a code manager without a dispatcher")
	}
	if got := len(bootpkg.GetHandlerRegistry(loaded).Handlers()); got != 0 {
		t.Fatalf("Load() registered %d handlers without a dispatcher, want 0", got)
	}
}

func TestL03EngineLoadRegistersProductionHandlers(t *testing.T) {
	ctx, bus := engineTestContext(t, zap.NewNop())
	loaded, err := Engine().Load(ctx)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if GetCodeManager(loaded) == nil {
		t.Fatal("Load() did not attach a code manager")
	}

	handlers := bootpkg.GetHandlerRegistry(loaded).Handlers()
	if len(handlers) != 5 {
		t.Fatalf("registered handlers = %d, want 5", len(handlers))
	}
	typeCounts := make(map[string]int)
	for _, handler := range handlers {
		typeCounts[fmt.Sprintf("%T", handler)]++
	}
	if typeCounts["*events.transactionAwareHandler"] != 1 || typeCounts["*component.Handler"] != 4 {
		t.Fatalf("registered handler types = %#v, want one transaction and four Lua component handlers", typeCounts)
	}

	kinds := []string{"function.lua.test", "library.lua.test", "process.lua.test", "workflow.lua.test"}
	for index, kind := range kinds {
		before := len(bus.events)
		id := registry.NewID("test", fmt.Sprintf("handler-%d", index))
		for _, handler := range handlers {
			if err := handler.Handle(loaded, event.Event{
				System: "lua",
				Kind:   "lua.reset_code",
				Data: luaapi.InvalidateNodesRequest{
					AckPrefix: "ack",
					Nodes:     []luaapi.InvalidateNode{{ID: id, Kind: kind}},
				},
			}); err != nil {
				t.Fatalf("handler for %q returned error: %v", kind, err)
			}
		}
		if got := len(bus.events) - before; got != 1 {
			t.Fatalf("handlers acknowledging %q = %d, want 1", kind, got)
		}
		if got := bus.events[len(bus.events)-1].Path; got != "ack/"+id.String() {
			t.Fatalf("ack path for %q = %q, want %q", kind, got, "ack/"+id.String())
		}
	}
}

func TestL04EngineLifecycleIdempotent(t *testing.T) {
	core, logs := observer.New(zap.InfoLevel)
	ctx, _ := engineTestContext(t, zap.New(core))
	component := Engine()
	loaded, err := component.Load(ctx)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	lifecycle, ok := component.(interface {
		Start(context.Context) error
		Stop(context.Context) error
	})
	if !ok {
		t.Fatal("Engine() does not implement lifecycle methods")
	}
	if err := lifecycle.Start(loaded); err != nil {
		t.Fatalf("first Start() error = %v", err)
	}
	if err := lifecycle.Start(loaded); err != nil {
		t.Fatalf("second Start() error = %v", err)
	}
	if err := lifecycle.Stop(loaded); err != nil {
		t.Fatalf("first Stop() error = %v", err)
	}
	if err := lifecycle.Stop(loaded); err != nil {
		t.Fatalf("second Stop() error = %v", err)
	}
	if got := logs.FilterMessage("function manager started").Len(); got != 1 {
		t.Fatalf("manager start transitions = %d, want 1", got)
	}
	if got := logs.FilterMessage("function manager stopped").Len(); got != 1 {
		t.Fatalf("manager stop transitions = %d, want 1", got)
	}

	absent, ok := Engine().(interface {
		Start(context.Context) error
		Stop(context.Context) error
	})
	if !ok {
		t.Fatal("Engine() without a manager does not implement lifecycle methods")
	}
	if err := absent.Start(context.Background()); err != nil {
		t.Fatalf("Start() without manager error = %v", err)
	}
	if err := absent.Stop(context.Background()); err != nil {
		t.Fatalf("Stop() without manager error = %v", err)
	}
}

func TestL05EngineSettingsDefaults(t *testing.T) {
	settings := resolveEngineSettings(nil)
	if settings.ProtoCacheSize != 60000 || settings.MainCacheSize != 10000 {
		t.Fatalf("cache sizes = (%d, %d), want (60000, 10000)", settings.ProtoCacheSize, settings.MainCacheSize)
	}
	if settings.TypeCheck.Enabled || settings.TypeCheck.Strict {
		t.Fatalf("type check defaults = %#v, want disabled and non-strict", settings.TypeCheck)
	}
	if settings.Cache.Enabled || settings.Cache.Dir != ".wippy/cache/lua" || string(settings.Cache.Mode) != "readwrite" {
		t.Fatalf("cache defaults = %#v", settings.Cache)
	}
	if !settings.Cache.CompileEnabled || !settings.Cache.TypecheckEnabled {
		t.Fatalf("cache stage defaults = %#v, want both enabled", settings.Cache)
	}
	if settings.InvalidationWaitTimeout != 30*time.Second {
		t.Fatalf("invalidation timeout = %v, want 30s", settings.InvalidationWaitTimeout)
	}
}

func TestL06EngineCacheEnablePrecedence(t *testing.T) {
	fixtures := []struct {
		values map[string]any
		want   bool
	}{
		{values: nil, want: false},
		{values: map[string]any{"type_system.enabled": true}, want: true},
		{values: map[string]any{"type_system.enabled": false, "cache.enabled": true}, want: true},
		{values: map[string]any{"type_system.enabled": true, "cache.enabled": false}, want: false},
	}
	for index, fixture := range fixtures {
		cfg := boot.NewConfig()
		if fixture.values != nil {
			cfg = boot.NewConfig(boot.WithSection("lua", fixture.values))
		}
		if got := resolveEngineSettings(cfg).Cache.Enabled; got != fixture.want {
			t.Fatalf("fixture %d cache enabled = %v, want %v", index, got, fixture.want)
		}
	}
}

func TestL07EngineRelativeCacheDirectory(t *testing.T) {
	cfg := boot.NewConfig(
		boot.WithSection("lua", map[string]any{"cache.dir": "state/lua"}),
		boot.WithSection("boot", map[string]any{"config_dir": "/srv/wippy/config"}),
	)
	want := filepath.Join("/srv/wippy/config", "state/lua")
	if got := resolveEngineSettings(cfg).Cache.Dir; got != want {
		t.Fatalf("relative cache directory = %q, want %q", got, want)
	}
}

func TestL08EngineAbsoluteCacheDirectory(t *testing.T) {
	absoluteCacheDir := filepath.Join(t.TempDir(), "cache")
	cfg := boot.NewConfig(
		boot.WithSection("lua", map[string]any{"cache.dir": absoluteCacheDir}),
		boot.WithSection("boot", map[string]any{"config_dir": filepath.Join(t.TempDir(), "config")}),
	)
	if got := resolveEngineSettings(cfg).Cache.Dir; got != absoluteCacheDir {
		t.Fatalf("absolute cache directory = %q, want %q", got, absoluteCacheDir)
	}
}

func TestL09EngineInvalidationTimeoutPrecedence(t *testing.T) {
	fixtures := []struct {
		lua      map[string]any
		registry map[string]any
		want     time.Duration
	}{
		{want: 30 * time.Second},
		{registry: map[string]any{"event_wait_timeout": "17s"}, want: 17 * time.Second},
		{lua: map[string]any{"invalidation_wait_timeout": "3s"}, registry: map[string]any{"event_wait_timeout": "17s"}, want: 3 * time.Second},
	}
	for index, fixture := range fixtures {
		opts := make([]boot.ConfigOption, 0, 2)
		if fixture.lua != nil {
			opts = append(opts, boot.WithSection("lua", fixture.lua))
		}
		if fixture.registry != nil {
			opts = append(opts, boot.WithSection("registry", fixture.registry))
		}
		if got := resolveEngineSettings(boot.NewConfig(opts...)).InvalidationWaitTimeout; got != fixture.want {
			t.Fatalf("fixture %d invalidation timeout = %v, want %v", index, got, fixture.want)
		}
	}
}

func TestL10LuaErrorMetadataProjectionAndDetach(t *testing.T) {
	details := map[string]any{"attempt": 7, "region": "test-zone"}
	err := apierror.NewRich(apierror.Kind("TransientLiteral"), "temporary failure").
		WithRetryable(apierror.True).
		WithDetails(details)

	metadata := extractLuaErrorMetadata(err)
	if metadata == nil {
		t.Fatal("extractLuaErrorMetadata() = nil")
	}
	if string(metadata.Kind) != "TransientLiteral" || metadata.Retryable == nil || !*metadata.Retryable {
		t.Fatalf("metadata fields = %#v", metadata)
	}
	if metadata.Details["attempt"] != 7 || metadata.Details["region"] != "test-zone" {
		t.Fatalf("metadata details = %#v", metadata.Details)
	}
	details["attempt"] = 8
	details["region"] = "changed"
	if metadata.Details["attempt"] != 7 || metadata.Details["region"] != "test-zone" {
		t.Fatalf("metadata details changed with source = %#v", metadata.Details)
	}
}

func TestL11LuaErrorMetadataNilCases(t *testing.T) {
	inputs := []error{nil, apierror.New(apierror.Kind(""), "metadata free")}
	for index, input := range inputs {
		if got := extractLuaErrorMetadata(input); got != nil {
			t.Fatalf("fixture %d metadata = %#v, want nil", index, got)
		}
	}
}
