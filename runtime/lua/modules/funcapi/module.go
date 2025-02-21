package function

import (
	"github.com/ponyruntime/pony/api/function"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/pubsub"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
	"strings"
)

// Module represents the function module for Lua
type Module struct {
	log *zap.Logger
}

// NewFuncContextModule creates a new function module
func NewFuncContextModule(log *zap.Logger) *Module {
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
		"pid":  m.pid,
		"send": m.send,
		//"inbox": m.inbox,
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

	// Parse arguments
	pidStr := l.CheckString(1)
	topic := l.CheckString(2)
	msgs := l.CheckTable(3) // Expect table of messages

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

	// Create message batch
	var messages []*pubsub.Message
	msgs.ForEach(func(_, value lua.LValue) {
		messages = append(messages, &pubsub.Message{
			Topic:    topic,
			Payloads: []payload.Payload{payload.NewPayload(value, payload.Lua)},
		})
	})

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

// inbox returns a channel for receiving messages
//func (m *Module) inbox(l *lua.LState) int {
//	fnCtx, ok := m.checkFunction(l)
//	if !ok {
//		return 2
//	}
//
//	// Get transcoder for message conversion
//	dtt := payload.GetTranscoder(l.Context())
//	if dtt == nil {
//		l.Push(lua.LNil)
//		l.Push(lua.LString("no transcoder found"))
//		return 2
//	}
//
//	// Create channel for receiving messages
//	ch := channel.Named("@msg", 1)
//
//	// Create a cancellable context
//	ctx, cancel := context.WithCancel(l.Context())
//
//	// Create inbox receiver
//	inbox := make(chan *pubsub.Package)
//	closer, err := pubsub.GetHost(ctx).Attach(fnCtx.PID, inbox)
//	if err != nil {
//		cancel()
//		l.Push(lua.LNil)
//		l.Push(lua.LString(err.Error()))
//		return 2
//	}
//
//	// Start goroutine to handle messages
//	go func() {
//		defer closer()
//		defer cancel()
//		defer async.Close(l, ch)
//
//		for {
//			select {
//			case pkg := <-inbox:
//				// Only handle @msg topic
//				for _, msg := range pkg.Messages {
//					if msg.Topic != "@msg" {
//						continue
//					}
//
//					// Convert payload to Lua value
//					for _, p := range msg.Payloads {
//						lv, err := dtt.Transcode(p, payload.Lua)
//						if err != nil {
//							m.log.Error("failed to transcode payload",
//								zap.Error(err),
//								zap.String("from", pkg.PID.String()))
//							continue
//						}
//
//						// Send to Lua channel
//						if err := async.Send(l, ch, lv.Data().(lua.LValue), true); err != nil {
//							m.log.Error("failed to send to channel",
//								zap.Error(err),
//								zap.String("from", pkg.PID.String()))
//							return
//						}
//					}
//				}
//			case <-ctx.Done():
//				return
//			}
//		}
//	}()
//
//	// Return channel to Lua
//	l.Push(channel.Wrap(l, ch))
//	return 1
//}
