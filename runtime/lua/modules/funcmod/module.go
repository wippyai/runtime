package funcmod

import (
	"context"
	"fmt"
	"strings"
	"sync"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/api/topology"
	"github.com/wippyai/runtime/runtime/lua/engine"
	"github.com/wippyai/runtime/runtime/lua/engine/channel"
	"github.com/wippyai/runtime/runtime/lua/engine/subscribe"
	"github.com/wippyai/runtime/runtime/lua/modules/process"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

// Channel context keys for UoW storage
var (
	subscribedKey = &ctxapi.Key{Name: "function.subscribed"}
	inboxChannel  = &ctxapi.Key{Name: "function.channel.inbox"}
	eventsChannel = &ctxapi.Key{Name: "function.channel.events"}
)

// Module provides inbox handling for short-lived functions and operations
type Module struct {
	log         *zap.Logger
	moduleTable *lua.LTable
	once        sync.Once
}

// NewFunctionAPIModule creates an overlay that starts listening on demand, perfect for lightweight functions
func NewFunctionAPIModule(log *zap.Logger) *Module {
	return &Module{
		log: log,
	}
}

// Info returns module metadata
func (e *Module) Info() luaapi.ModuleInfo {
	return luaapi.ModuleInfo{
		Name:        "function_api",
		Description: "Function API with lazy inbox handling",
		Class:       []string{luaapi.ClassProcess, luaapi.ClassNondeterministic},
	}
}

// Loader is the entry point for loading the module into Lua
func (e *Module) Loader(l *lua.LState) int {
	e.once.Do(func() {
		// Get the process module to create base table
		processModule := process.NewProcessAPIModule(e.log)

		// Create fresh mutable base table with all core process functions
		mod := processModule.NewProcessTable(l)

		// Add function-specific methods to the base table
		mod.RawSetString("inbox", l.NewFunction(e.lazyInbox))
		mod.RawSetString("events", l.NewFunction(e.lazyEvents))
		mod.RawSetString("listen", l.NewFunction(e.lazyListen))
		mod.RawSetString("unlisten", l.NewFunction(e.lazyUnlisten))

		// Make immutable for reuse across all functions
		mod.Immutable = true
		e.moduleTable = mod
	})

	l.Push(e.moduleTable)
	return 1
}

// ensureSubscriptions sets up message handling for the process
func (e *Module) ensureSubscriptions(l *lua.LState) bool {
	uw := engine.GetUnitOfWork(l.Context())
	if uw == nil {
		e.log.Error("no unit of work found")
		return false
	}

	// Check if we've already set up subscriptions for this UoW
	if _, found := uw.Values().Get(subscribedKey); found {
		return true
	}

	// Get PID from frame context
	pid, ok := runtime.GetFramePID(l.Context())
	if !ok {
		e.log.Error("no PID found in frame context")
		return false
	}

	// Create inbox receiver
	inbox := make(chan *relay.Package, 1)
	closer, err := relay.GetNode(uw.Context()).Attach(pid, inbox)
	if err != nil {
		e.log.Error("failed to attach to node", zap.Error(err))
		return false
	}

	uw.Run(func(uw engine.UnitOfWork) {
		defer func() {
			closer()

			// drain the inbox
			for {
				select {
				case pkg := <-inbox:
					relay.ReleasePackage(pkg)
				default:
					topology.GetTopology(uw.Context()).Remove(pid)
					return
				}
			}
		}()

		for {
			select {
			case <-uw.Context().Done():
				return
			case pkg := <-inbox:
				e.processPackage(uw, pkg)
			}
		}
	})

	// Mark as subscribed
	uw.Values().Set(subscribedKey, true)
	return true
}

// processPackage handles an incoming message package
func (e *Module) processPackage(uw engine.UnitOfWork, pkg *relay.Package) {
	if pkg == nil {
		return
	}

	for _, msg := range pkg.Messages {
		// Always forward topology events
		if msg.Topic == topology.TopicEvents {
			// First check if events channel exists
			luaValues, err := e.toLuaPayloads(uw.Context(), msg.Payloads)
			if err == nil && len(luaValues) > 0 {
				if err := subscribe.Publish(uw.Context(), topology.TopicEvents, luaValues...); err != nil {
					e.log.Error("failed to publish event", zap.Error(err))
				}
			}

			continue
		}

		// Check if the topic has a specific channel
		if exists, _ := subscribe.Exists(uw.Context(), msg.Topic); exists {
			luaValues, err := e.toLuaPayloads(uw.Context(), msg.Payloads)
			if err != nil {
				e.log.Error("failed to convert payloads", zap.Error(err))
				continue
			}

			if len(luaValues) == 0 {
				continue
			}

			if err := subscribe.Publish(uw.Context(), msg.Topic, luaValues...); err != nil {
				e.log.Error("failed to publish message",
					zap.String("topic", msg.Topic),
					zap.Error(err))
			}
			continue
		}

		inboxValues := make([]lua.LValue, 0, len(msg.Payloads))
		for _, p := range msg.Payloads {
			m := process.NewMessage(pkg.Source, msg.Topic, p)
			inboxValues = append(inboxValues, process.WrapMessage(uw.State(), m))
		}

		// has internal queue, but must be drained
		if err := subscribe.Publish(uw.Context(), topology.TopicInbox, inboxValues...); err != nil {
			e.log.Error("failed to publish to inbox",
				zap.String("pid", pkg.Target.String()),
				zap.String("topic", topology.TopicInbox),
				zap.Error(err))
		}
	}

	relay.ReleasePackage(pkg)
}

// toLuaPayloads converts a slice of payloads to Lua values
func (e *Module) toLuaPayloads(ctx context.Context, payloads payload.Payloads) ([]lua.LValue, error) {
	dtt := payload.GetTranscoder(ctx)
	if dtt == nil {
		return nil, nil
	}

	args := make([]lua.LValue, 0, len(payloads))
	for _, pp := range payloads {
		luaPayload, err := dtt.Transcode(pp, payload.Lua)
		if err != nil {
			return nil, err
		}

		if lv, ok := luaPayload.Data().(lua.LValue); ok {
			args = append(args, lv)
		}
	}

	return args, nil
}

// lazyInbox handles inbox initialization on demand
func (e *Module) lazyInbox(l *lua.LState) int {
	// Ensure subscriptions are set up
	if !e.ensureSubscriptions(l) {
		l.Push(lua.LNil)
		l.Push(lua.LString("failed to set up message handling"))
		return 2
	}

	// Get UoW from context
	uw := engine.GetUnitOfWork(l.Context())
	if uw == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no unit of work found"))
		return 2
	}

	// Check if channel already exists in UoW
	existingChannel, found := uw.Values().Get(inboxChannel)
	if found {
		l.Push(existingChannel.(lua.LValue))
		return 1
	}

	ch := channel.Named(topology.TopicInbox, 0)
	result := subscribe.Subscribe(l, ch, topology.TopicInbox)

	// If subscription was successful, store the channel in UoW
	if result == 1 {
		// The channel is returned on the Lua stack, we need to get it
		channelWrapper := l.Get(-1)
		uw.Values().Set(inboxChannel, channelWrapper)
	}

	return result
}

// lazyEvents handles events channel initialization on demand
func (e *Module) lazyEvents(l *lua.LState) int {
	// Ensure subscriptions are set up
	if !e.ensureSubscriptions(l) {
		l.Push(lua.LNil)
		l.Push(lua.LString("failed to set up message handling"))
		return 2
	}

	// Get UoW from context
	uw := engine.GetUnitOfWork(l.Context())
	if uw == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no unit of work found"))
		return 2
	}

	// Check if channel already exists in UoW
	existingChannel, found := uw.Values().Get(eventsChannel)
	if found {
		l.Push(existingChannel.(lua.LValue))
		return 1
	}

	// Create new channel for events
	ch := channel.Named(topology.TopicEvents, 0)
	result := subscribe.Subscribe(l, ch, topology.TopicEvents)

	// If subscription was successful, store the channel in UoW
	if result == 1 {
		// The channel is returned on the Lua stack, we need to get it
		channelWrapper := l.Get(-1)
		uw.Values().Set(eventsChannel, channelWrapper)
	}

	return result
}

// lazyListen handles topic-specific channel initialization on demand
func (e *Module) lazyListen(l *lua.LState) int {
	topic := l.CheckString(1)
	if topic == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("topic cannot be empty"))
		return 2
	}

	// Prevent usage of @ topics in ports
	if strings.HasPrefix(topic, "@") {
		l.Push(lua.LNil)
		l.Push(lua.LString("cannot use @ topics"))
		return 2
	}

	// Ensure subscriptions are set up
	if !e.ensureSubscriptions(l) {
		l.Push(lua.LNil)
		l.Push(lua.LString("failed to set up message handling"))
		return 2
	}

	// Create new channel for the topic - NOT cached
	portName := fmt.Sprintf("listen.%s", topic)
	ch := channel.Named(portName, 1)

	// Return the subscription result directly
	return subscribe.Subscribe(l, ch, topic)
}

// lazyUnlisten unsubscribes from a topic via channel
func (e *Module) lazyUnlisten(l *lua.LState) int {
	ch := channel.CheckChannel(l)
	return subscribe.Unsubscribe(l, ch)
}
