package asyncinbox

import (
	"github.com/ponyruntime/pony/api/context"
	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/api/topology"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
	"github.com/ponyruntime/pony/runtime/lua/modules/process"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

var (
	// inboxChannel is the context key for the inbox channel
	inboxChannel = &context.Key{Name: "lua.async_inbox"}
)

// Module provides inbox handling for short-lived functions and operations
type Module struct {
	log *zap.Logger
}

// NewAsyncInbox creates a new extension for function inboxes
func NewAsyncInbox(log *zap.Logger) *Module {
	return &Module{
		log: log,
	}
}

// Name returns the module name
func (e *Module) Name() string {
	return "async_inbox"
}

// Loader is the entry point for loading the module into Lua
func (e *Module) Loader(l *lua.LState) int {
	// Register message type
	process.RegisterMessageType(l)

	// Find the process table
	v := l.GetGlobal("process")
	if v.Type() == lua.LTTable {
		// Get process table
		processTable := v.(*lua.LTable)
		processTable.RawSetString("inbox", l.NewFunction(e.inbox))
	} else {
		e.log.Error("process table not found")
	}

	return 0
}

// inbox creates an inbox channel for receiving messages
func (e *Module) inbox(l *lua.LState) int {
	pid, ok := pubsub.GetPID(l.Context())
	if !ok {
		l.Push(lua.LNil)
		l.Push(lua.LString("no PID found"))
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

	// Create channel for receiving messages
	ch := channel.Named("async_inbox", 0)

	// Create inbox receiver
	inbox := make(chan *pubsub.Package, 1)
	closer, err := pubsub.GetNode(uw.Context()).Attach(pid, inbox)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	uw.Run(func(uw engine.UnitOfWork) {
		defer func() {
			closer()

			// drain the inbox
			for {
				select {
				case pkg := <-inbox:
					pubsub.ReleasePackage(pkg)
				default:
					// todo: we can pass error here
					topology.GetTopology(uw.Context()).Remove(pid)
					return
				}
			}
		}()

		for {
			select {
			case pkg := <-inbox:
				// Handle all messages and topics
				for _, msg := range pkg.Messages {
					for _, p := range msg.Payloads {
						lMsg := process.NewMessage(msg.Topic, p)

						if err := channel.Send(l, ch, process.WrapMessage(l, lMsg)); err != nil {
							pubsub.ReleasePackage(pkg)
							e.log.Error("failed to send to channel",
								zap.Error(err),
								zap.String("pid", pid.String()))
							return
						}
					}
				}
				pubsub.ReleasePackage(pkg)
			case <-uw.Context().Done():
				return
			}
		}
	})

	// Store channel wrapper in UoW
	channelWrapper := channel.Wrap(l, ch)
	uw.Values().Set(inboxChannel, channelWrapper)

	// Return channel to Lua
	l.Push(channelWrapper)
	return 1
}
