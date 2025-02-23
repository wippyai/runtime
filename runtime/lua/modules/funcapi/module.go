package function

import (
	"github.com/ponyruntime/pony/api/context"
	"github.com/ponyruntime/pony/api/function"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/runtime/lua/engine/async"
	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
	"github.com/ponyruntime/pony/runtime/uow"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
	"strings"
)

var inboxChannel = &context.Key{Name: "lua.function_inbox"}

// Module represents the function module for Lua
type Module struct {
	log *zap.Logger
}

// NewFuncAPIModule creates a new function module
func NewFuncAPIModule(log *zap.Logger) *Module {
	return &Module{
		log: log,
	}
}

// Name returns the module name
func (m *Module) Name() string {
	return "func"
}

// Loader is the entry point for loading the module into Lua
func (m *Module) Loader(l *lua.LState) int {
	// Create module table
	mod := l.NewTable()

	// Register functions
	l.SetFuncs(mod, map[string]lua.LGFunction{
		"pid":   m.pid,
		"send":  m.send,
		"inbox": m.inbox,
	})

	l.Push(mod)
	return 1
}

func (m *Module) getNode(l *lua.LState) (pubsub.Node, bool) {
	ctx := l.Context()
	if ctx == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no context found"))
		return nil, false
	}

	node := pubsub.GetNode(ctx)
	if node == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no node found in context"))
		return nil, false
	}

	return node, true
}

// checkFunction validates context and returns function context if valid
func (m *Module) checkFunction(l *lua.LState) (*function.Context, bool) {
	ctx := l.Context()
	if ctx == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no context found"))
		return nil, false
	}

	fnCtx := function.GetContext(ctx)
	if fnCtx == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no function context found"))
		return nil, false
	}

	return fnCtx, true
}

// pid returns current function's PID
func (m *Module) pid(l *lua.LState) int {
	fnCtx, ok := m.checkFunction(l)
	if !ok {
		return 2
	}

	l.Push(lua.LString(fnCtx.PID.String()))
	return 1
}

// send sends a message to another function or process
func (m *Module) send(l *lua.LState) int {
	node, ok := m.getNode(l)
	if !ok {
		return 2
	}

	self, ok := m.checkFunction(l)
	if !ok {
		return 2
	}

	// Parse required arguments
	pidStr := l.CheckString(1)
	topic := l.CheckString(2)

	// Validate topic - prevent @ topics
	if strings.HasPrefix(topic, "@") {
		l.Push(lua.LNil)
		l.Push(lua.LString("cannot send to @ topics"))
		return 2
	}

	// Parse PID
	pid, err := pubsub.ParsePID(pidStr)
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	// Create message batch from variadic arguments
	var messages []*pubsub.Message
	for i := 3; i <= l.GetTop(); i++ {
		messages = append(messages, &pubsub.Message{
			Topic:    topic,
			Payloads: []payload.Payload{payload.NewPayload(l.Get(i), payload.Lua)},
		})
	}

	// Create package with all messages
	pkg := &pubsub.Package{
		PID:      pid,
		Messages: messages,
	}

	// Send message using node
	if err := node.Send(pkg); err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	m.log.Debug("function messages sent",
		zap.String("from", self.PID.String()),
		zap.String("to", pid.String()),
		zap.String("topic", topic),
		zap.Int("count", len(messages)),
	)

	l.Push(lua.LTrue)
	return 1
}

func (m *Module) inbox(l *lua.LState) int {
	fnCtx, ok := m.checkFunction(l)
	if !ok {
		return 2
	}

	// Get UoW from context
	uw := uow.FromContext(l.Context())
	if uw == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no unit of work found"))
		return 2
	}

	// Check if channel already exists in UoW
	existingChannel, found := uw.Get(inboxChannel)
	if found {
		l.Push(existingChannel.(lua.LValue))
		return 1
	}

	// Get transcoder for message conversion
	dtt := payload.GetTranscoder(l.Context())
	if dtt == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("no transcoder found"))
		return 2
	}

	// Create channel for receiving messages (not named)
	ch := channel.Named("function_inbox", 0)

	// Create inbox receiver
	inbox := make(chan *pubsub.Package)
	closer, err := pubsub.GetNode(uw.Context()).Attach(fnCtx.PID, inbox)

	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	// Register cleanup in UoW
	uw.AddCleanup(func() error {
		closer()
		return async.Close(l, ch)
	})

	// Start goroutine only once
	go func() {
		for {
			select {
			case pkg := <-inbox:
				// Handle all messages and topics
				for _, msg := range pkg.Messages {
					for _, p := range msg.Payloads {
						lv, err := dtt.Transcode(p, payload.Lua)
						if err != nil {
							m.log.Error("failed to transcode payload",
								zap.Error(err),
								zap.String("from", pkg.PID.String()))
							continue
						}

						// Create message table with payload and topic
						msgTable := l.NewTable()
						msgTable.RawSetString("topic", lua.LString(msg.Topic))
						msgTable.RawSetString("payload", lv.Data().(lua.LValue))

						// Send table to Lua channel
						if err := async.Send(l, ch, msgTable, true); err != nil {
							m.log.Error("failed to send to channel",
								zap.Error(err),
								zap.String("pid", pkg.PID.String()))
							return
						}
					}
				}
			case <-uw.Done():
				return
			}
		}
	}()

	// Store channel wrapper in UoW
	channelWrapper := channel.Wrap(l, ch)
	uw.Set(inboxChannel, channelWrapper)

	// Return channel to Lua
	l.Push(channelWrapper)
	return 1
}
