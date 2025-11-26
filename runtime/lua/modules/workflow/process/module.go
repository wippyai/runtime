package process

import (
	"fmt"
	"strings"
	"sync"

	"github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/runtime"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/api/topology"
	"github.com/wippyai/runtime/api/workflow/std"
	"github.com/wippyai/runtime/runtime/lua/engine"
	"github.com/wippyai/runtime/runtime/lua/engine/channel"
	"github.com/wippyai/runtime/runtime/lua/engine/subscribe"
	processbase "github.com/wippyai/runtime/runtime/lua/modules/process"
	"github.com/wippyai/runtime/runtime/lua/modules/upstream"
	luaconv "github.com/wippyai/runtime/system/payload/lua"
	lua "github.com/yuin/gopher-lua"
)

var (
	inboxChannel  = &context.Key{Name: "workflow.process.channel.inbox"}
	eventsChannel = &context.Key{Name: "workflow.process.channel.events"}
	tasksChannel  = &context.Key{Name: "workflow.process.channel.tasks"}
)

// TopicTasks is the internal topic for workflow tasks from host
const TopicTasks = "@workflow/tasks"

// Module provides workflow-safe process API
type Module struct {
	moduleTable *lua.LTable
	once        sync.Once
}

// NewProcessModule creates a new workflow process module
func NewProcessModule() *Module {
	return &Module{}
}

// Info returns module metadata
func (m *Module) Info() luaapi.ModuleInfo {
	return luaapi.ModuleInfo{
		Name:        "workflow.process",
		Description: "Workflow-safe process API",
		Class:       []string{luaapi.ClassWorkflow, luaapi.ClassProcess},
	}
}

// Loader registers the module functions
func (m *Module) Loader(l *lua.LState) int {
	m.once.Do(func() {
		m.initModuleTable(l)
	})

	l.Push(m.moduleTable)
	return 1
}

func (m *Module) initModuleTable(l *lua.LState) {
	mod := l.CreateTable(0, 16)

	// Read-only functions - delegate to base
	mod.RawSetString("id", l.NewFunction(m.id))
	mod.RawSetString("pid", l.NewFunction(m.pid))

	// Messaging - send via command
	mod.RawSetString("send", l.NewFunction(m.send))

	// Channel-based functions - work with named channels
	mod.RawSetString("inbox", l.NewFunction(m.inbox))
	mod.RawSetString("events", l.NewFunction(m.events))
	mod.RawSetString("tasks", l.NewFunction(m.tasks))
	mod.RawSetString("listen", l.NewFunction(m.listen))
	mod.RawSetString("unlisten", l.NewFunction(m.unlisten))

	// Disabled functions - raise error
	mod.RawSetString("spawn", l.NewFunction(m.disabledSpawn))
	mod.RawSetString("spawn_monitored", l.NewFunction(m.disabledSpawn))
	mod.RawSetString("spawn_linked", l.NewFunction(m.disabledSpawn))
	mod.RawSetString("spawn_linked_monitored", l.NewFunction(m.disabledSpawn))
	mod.RawSetString("terminate", l.NewFunction(m.disabledTerminate))
	mod.RawSetString("cancel", l.NewFunction(m.disabledCancel))
	mod.RawSetString("monitor", l.NewFunction(m.disabledTopology))
	mod.RawSetString("unmonitor", l.NewFunction(m.disabledTopology))
	mod.RawSetString("link", l.NewFunction(m.disabledTopology))
	mod.RawSetString("unlink", l.NewFunction(m.disabledTopology))
	mod.RawSetString("with_context", l.NewFunction(m.disabledWithContext))

	// Read-only options
	mod.RawSetString("get_options", l.NewFunction(m.getOptions))
	mod.RawSetString("set_options", l.NewFunction(m.disabledSetOptions))

	// Event constants
	events := l.CreateTable(0, 3)
	events.RawSetString("CANCEL", lua.LString(topology.KindCancel))
	events.RawSetString("EXIT", lua.LString(topology.KindExit))
	events.RawSetString("LINK_DOWN", lua.LString(topology.KindLinkDown))
	events.Immutable = true
	mod.RawSetString("event", events)

	// Registry - disabled in workflows
	reg := l.CreateTable(0, 3)
	reg.RawSetString("register", l.NewFunction(m.disabledRegistry))
	reg.RawSetString("lookup", l.NewFunction(m.disabledRegistry))
	reg.RawSetString("unregister", l.NewFunction(m.disabledRegistry))
	reg.Immutable = true
	mod.RawSetString("registry", reg)

	// Register message type from base module
	processbase.RegisterMessageType(l)

	mod.Immutable = true
	m.moduleTable = mod
}

// id returns the process ID (registry ID)
func (m *Module) id(l *lua.LState) int {
	ctx := l.Context()
	if ctx == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no context found"))
		return 2
	}

	cc := context.FrameFromContext(ctx)
	if cc == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("FrameContext not found"))
		return 2
	}

	idValue, ok := cc.Get(runtime.FrameIDKey)
	if !ok {
		l.Push(lua.LNil)
		l.Push(lua.LString("call ID not found in context"))
		return 2
	}

	callID, ok := idValue.(registry.ID)
	if !ok {
		l.Push(lua.LNil)
		l.Push(lua.LString("invalid call ID type"))
		return 2
	}

	l.Push(lua.LString(callID.String()))
	return 1
}

// pid returns the process PID string
func (m *Module) pid(l *lua.LState) int {
	pid, ok := runtime.GetFramePID(l.Context())
	if !ok {
		l.Push(lua.LNil)
		return 1
	}

	l.Push(lua.LString(pid.String()))
	return 1
}

// send sends a message to another process via command (non-blocking)
func (m *Module) send(l *lua.LState) int {
	if l.GetTop() < 2 {
		l.Push(lua.LNil)
		l.Push(lua.LString("send requires at least destination and topic arguments"))
		return 2
	}

	pidOrName := l.CheckString(1)
	topic := l.CheckString(2)

	if strings.HasPrefix(topic, "@") {
		l.Push(lua.LNil)
		l.Push(lua.LString("cannot send to @ topics"))
		return 2
	}

	// Build typed header
	header := &std.ProcessSendHeader{
		Target: pidOrName,
		Topic:  topic,
	}

	// Build payloads: header first, then message content
	payloads := []payload.Payload{payload.New(header)}
	for i := 3; i <= l.GetTop(); i++ {
		payloads = append(payloads, luaconv.ExportPayload(l.Get(i)))
	}

	// Create request
	req := upstream.NewRequest(l, std.TypeProcessSend, nil, payloads...)

	// Send to upstream (non-blocking)
	up, ok := runtime.GetUpstream(l.Context())
	if !ok {
		l.Push(lua.LNil)
		l.Push(lua.LString("no upstream handler found in context"))
		return 2
	}

	if err := up.SendRequest(req); err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("failed to send request: %s", err.Error())))
		return 2
	}

	l.Push(lua.LTrue)
	return 1
}

// inbox creates the inbox channel for receiving messages
func (m *Module) inbox(l *lua.LState) int {
	uw := engine.GetUnitOfWork(l.Context())
	if uw == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no unit of work found"))
		return 2
	}

	existingChannel, found := uw.Values().Get(inboxChannel)
	if found {
		l.Push(existingChannel.(lua.LValue))
		return 1
	}

	ch := channel.Named(topology.TopicInbox, 0)
	result := subscribe.Subscribe(l, ch, topology.TopicInbox)

	if result == 1 {
		channelWrapper := l.Get(-1)
		uw.Values().Set(inboxChannel, channelWrapper)
	}

	return result
}

// events creates the events channel
func (m *Module) events(l *lua.LState) int {
	uw := engine.GetUnitOfWork(l.Context())
	if uw == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no unit of work found"))
		return 2
	}

	existingChannel, found := uw.Values().Get(eventsChannel)
	if found {
		l.Push(existingChannel.(lua.LValue))
		return 1
	}

	ch := channel.Named(topology.TopicEvents, 0)
	result := subscribe.Subscribe(l, ch, topology.TopicEvents)

	if result == 1 {
		channelWrapper := l.Get(-1)
		uw.Values().Set(eventsChannel, channelWrapper)
	}

	return result
}

// tasks creates the tasks channel for receiving host tasks (queries, updates)
func (m *Module) tasks(l *lua.LState) int {
	uw := engine.GetUnitOfWork(l.Context())
	if uw == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no unit of work found"))
		return 2
	}

	existingChannel, found := uw.Values().Get(tasksChannel)
	if found {
		l.Push(existingChannel.(lua.LValue))
		return 1
	}

	ch := channel.Named(TopicTasks, 0)
	result := subscribe.Subscribe(l, ch, TopicTasks)

	if result == 1 {
		channelWrapper := l.Get(-1)
		uw.Values().Set(tasksChannel, channelWrapper)
	}

	return result
}

// listen creates a channel for specific topic listening
func (m *Module) listen(l *lua.LState) int {
	topic := l.CheckString(1)
	if topic == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("topic cannot be empty"))
		return 2
	}

	if strings.HasPrefix(topic, "@") {
		l.Push(lua.LNil)
		l.Push(lua.LString("cannot use @ topics"))
		return 2
	}

	portName := fmt.Sprintf("listen.%s", topic)
	ch := channel.Named(portName, 1)
	return subscribe.Subscribe(l, ch, topic)
}

// unlisten unsubscribes from a topic
func (m *Module) unlisten(l *lua.LState) int {
	ch := channel.CheckChannel(l)
	return subscribe.Unsubscribe(l, ch)
}

// getOptions returns empty options (read-only)
func (m *Module) getOptions(l *lua.LState) int {
	options := l.CreateTable(0, 0)
	l.Push(options)
	return 1
}

// Disabled functions

func (m *Module) disabledSpawn(l *lua.LState) int {
	l.RaiseError("spawn is not allowed in workflows - use funcs:async() to execute activities instead")
	return 0
}

func (m *Module) disabledTerminate(l *lua.LState) int {
	l.RaiseError("terminate is not allowed in workflows")
	return 0
}

func (m *Module) disabledCancel(l *lua.LState) int {
	l.RaiseError("cancel is not allowed in workflows")
	return 0
}

func (m *Module) disabledTopology(l *lua.LState) int {
	l.RaiseError("topology operations (monitor/link) are not allowed in workflows")
	return 0
}

func (m *Module) disabledWithContext(l *lua.LState) int {
	l.RaiseError("with_context is not allowed in workflows - use funcs:call() with context instead")
	return 0
}

func (m *Module) disabledSetOptions(l *lua.LState) int {
	l.Push(lua.LBool(false))
	l.Push(lua.LString("set_options is not allowed in workflows"))
	return 2
}

func (m *Module) disabledRegistry(l *lua.LState) int {
	l.RaiseError("registry operations are not allowed in workflows")
	return 0
}
