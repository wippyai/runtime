package events_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/api/security"
	"github.com/wippyai/runtime/internal/uniqid"
	"github.com/wippyai/runtime/runtime/lua/engine"
	eventsmod "github.com/wippyai/runtime/runtime/lua/modules/events"
	timemod "github.com/wippyai/runtime/runtime/lua/modules/time"
	"github.com/wippyai/runtime/system/clock"
	"github.com/wippyai/runtime/system/eventbus"
	sysrelay "github.com/wippyai/runtime/system/relay"
	"github.com/wippyai/runtime/system/scheduler"
	"github.com/wippyai/runtime/system/scheduler/pool/inline"
	lua "github.com/yuin/gopher-lua"
)

// DebugPool wraps pool to log Send
type DebugPool struct {
	*inline.Pool
	t *testing.T
}

func (d *DebugPool) Send(pkg *relay.Package) error {
	var topic string
	if len(pkg.Messages) > 0 {
		topic = pkg.Messages[0].Topic
	}
	d.t.Logf("Pool.Send: Target.UniqID=%s Topic=%s", pkg.Target.UniqID, topic)
	err := d.Pool.Send(pkg)
	d.t.Logf("Pool.Send result: err=%v", err)
	return err
}

func TestEventsReceiveIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	rootCtx := security.SetStrictMode(ctxapi.NewRootContext(), false)
	ctx, cancel := context.WithTimeout(rootCtx, 5*time.Second)
	defer cancel()

	// Create event bus
	bus := eventbus.NewBus()

	// Create relay node
	realNode := sysrelay.NewNode("test-node")

	// Create dispatcher registry
	reg := scheduler.NewRegistry()

	// Create clock dispatcher
	clockSvc := clock.NewDispatcher()
	defer func() { _ = clockSvc.Stop(ctx) }()
	clockSvc.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		reg.Register(id, h)
	})

	// Create events dispatcher
	eventsSvc := eventbus.NewDispatcher(bus, realNode)
	err := eventsSvc.Start(ctx)
	require.NoError(t, err)
	defer func() { _ = eventsSvc.Stop(ctx) }()

	eventsSvc.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		reg.Register(id, h)
	})

	// Create PID generator
	uniqGen := uniqid.NewGenerator()
	pidGen := uniqid.NewPIDGenerator(uniqGen, "test-node")

	hostID := "test.events:receive"

	// Create process factory with events and time modules
	factory := func() (process.Process, error) {
		cfg := engine.FactoryConfig{
			Script: `
local events = require("events")
local time = require("time")

local function main()
    local sub, err = events.subscribe("test.system")
    if err then
        return nil, "subscribe error: " .. tostring(err)
    end

    local ch = sub:channel()

    -- Send event directly (no spawn)
    events.send("test.system", "test.kind", "/test/path", {key = "value"})

    -- Wait for event with timeout (use proper milliseconds)
    local timer = time.after(2000 * time.MILLISECOND)
    local result = channel.select{
        ch:case_receive(),
        timer:case_receive()
    }

    if result.channel == timer then
        return nil, "timeout waiting for event"
    end

    local evt = result.value
    if evt == nil then
        return nil, "no event value"
    end
    if evt.system ~= "test.system" then
        return nil, "wrong system: " .. tostring(evt.system)
    end
    if evt.kind ~= "test.kind" then
        return nil, "wrong kind: " .. tostring(evt.kind)
    end
    if evt.path ~= "/test/path" then
        return nil, "wrong path: " .. tostring(evt.path)
    end

    return true
end

return { main = main }
`,
			ScriptName: "test_events",
			ModuleBinders: append(engine.CoreBinders(),
				func(l *lua.LState) {
					mod, _ := eventsmod.Module.Build()
					l.PreloadModule("events", func(L *lua.LState) int {
						L.Push(mod)
						return 1
					})
				},
				func(l *lua.LState) {
					mod, _ := timemod.Module.Build()
					l.PreloadModule("time", func(L *lua.LState) int {
						L.Push(mod)
						return 1
					})
				},
			),
		}
		f := engine.NewFactory(cfg)
		return f()
	}

	// Create inline pool
	realPool, err := inline.New(factory, reg)
	require.NoError(t, err)
	defer realPool.Stop()

	inlinePool := &DebugPool{Inline: realPool, t: t}

	// Register pool as relay host
	err = realNode.RegisterHost(hostID, inlinePool)
	require.NoError(t, err)
	t.Logf("Registered pool as host: %s", hostID)

	// Create frame context with PID and relay node
	frameCtx, fc := ctxapi.AcquireFrameContext(ctx)
	defer ctxapi.ReleaseFrameContext(fc)

	pid := pidGen.Generate(hostID)
	t.Logf("Generated PID: Host=%s UniqID=%s", pid.Host, pid.UniqID)
	err = runtime.SetFramePID(frameCtx, pid)
	require.NoError(t, err)

	// Set relay node in context for timer callbacks
	frameCtx = relay.WithNode(frameCtx, realNode)

	// Execute
	result, err := realPool.Call(frameCtx, "main", nil)
	require.NoError(t, err)
	require.NotNil(t, result)

	if result.Error != nil {
		t.Logf("Execution error: %v", result.Error)
	}
	require.NoError(t, result.Error)

	// Check result
	if result.Value != nil {
		val := result.Value.Data()
		t.Logf("Result: %v (type: %T)", val, val)
	}
}

func TestEventsBasicSubscribe(t *testing.T) {
	rootCtx := security.SetStrictMode(ctxapi.NewRootContext(), false)
	ctx, cancel := context.WithTimeout(rootCtx, 2*time.Second)
	defer cancel()

	// Create event bus
	bus := eventbus.NewBus()

	// Create relay node
	node := sysrelay.NewNode("test-node")

	// Create dispatcher registry
	reg := scheduler.NewRegistry()

	// Create events dispatcher
	eventsSvc := eventbus.NewDispatcher(bus, node)
	err := eventsSvc.Start(ctx)
	require.NoError(t, err)
	defer func() { _ = eventsSvc.Stop(ctx) }()

	eventsSvc.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		reg.Register(id, h)
	})

	// Create PID generator
	uniqGen := uniqid.NewGenerator()
	pidGen := uniqid.NewPIDGenerator(uniqGen, "test-node")

	// Create process factory
	factory := func() (process.Process, error) {
		cfg := engine.FactoryConfig{
			Script: `
local events = require("events")

local function main()
    local ch, err = events.subscribe("test.*")
    if err then
        return nil, err
    end
    if not ch then
        return nil, "channel is nil"
    end
    return true
end

return { main = main }
`,
			ScriptName: "test_subscribe",
			ModuleBinders: append(engine.CoreBinders(),
				func(l *lua.LState) {
					mod, _ := eventsmod.Module.Build()
					l.PreloadModule("events", func(L *lua.LState) int {
						L.Push(mod)
						return 1
					})
				},
			),
		}
		f := engine.NewFactory(cfg)
		return f()
	}

	// Create inline pool
	inlinePool, err := inline.New(factory, reg)
	require.NoError(t, err)
	defer inlinePool.Stop()

	// Register pool as relay host
	hostID := "test.events:subscribe"
	err = node.RegisterHost(hostID, inlinePool)
	require.NoError(t, err)

	// Create frame context with PID
	frameCtx, fc := ctxapi.AcquireFrameContext(ctx)
	defer ctxapi.ReleaseFrameContext(fc)

	pid := pidGen.Generate(hostID)
	err = runtime.SetFramePID(frameCtx, pid)
	require.NoError(t, err)

	// Execute
	result, err := inlinePool.Call(frameCtx, "main", nil)
	require.NoError(t, err)
	require.NotNil(t, result)
	require.NoError(t, result.Error)
}
