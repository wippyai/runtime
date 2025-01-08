package channel

import (
	lua "github.com/yuin/gopher-lua"
)

// Module represents a channel Lua module
type Module struct{}

// NewChannelModule creates and returns a new instance of the channel Module
func NewChannelModule() *Module {
	return &Module{}
}

// Name returns the module's name
func (m *Module) Name() string {
	return "channel"
}

// Loader registers the module's functions into Lua state
func (m *Module) Loader(L *lua.LState) int {
	// Create module table
	mod := L.NewTable()

	// Create channel metatable
	mt := L.NewTypeMetatable("channel")
	L.SetField(mt, "__index", mt)

	// channel constructor
	L.SetField(mod, "new", L.NewFunction(m.new))
	L.SetField(mod, "external", L.NewFunction(m.newExternal)) // todo: remove it later

	// channel operations
	L.SetField(mt, "send", L.NewFunction(m.send))
	L.SetField(mt, "receive", L.NewFunction(m.receive))
	L.SetField(mt, "close", L.NewFunction(m.close))

	//
	L.SetField(mod, "select", L.NewFunction(selectOp))
	L.SetField(mt, "case_send", L.NewFunction(caseSend))
	L.SetField(mt, "case_receive", L.NewFunction(caseReceive))

	// no need for require
	L.SetGlobal("channel", mod)

	L.Push(mod)

	return 1
}

// new implements channel.new(capacity)
func (m *Module) new(L *lua.LState) int {
	capacity := L.OptInt(1, 0)
	if capacity < 0 {
		L.RaiseError("channel capacity must be >= 0")
		return 0
	}

	ch := newLuaChannel(capacity)
	ud := L.NewUserData()
	ud.Value = ch

	L.SetMetatable(ud, L.GetTypeMetatable("channel"))
	L.Push(ud)

	return 1
}

// send implements channel:send(value)
func (m *Module) send(L *lua.LState) int {
	ch := L.CheckUserData(1).Value.(*Channel)
	value := L.CheckAny(2)

	if ch.IsExternal() {
		L.RaiseError("cannot send to external channel")
		return 0
	}

	if ch.closed {
		L.RaiseError("attempt to send on closed channel")
		return 0
	}

	// For buffered channels, try to send immediately
	if ch.capacity > 0 && !ch.isFull() {
		ok := ch.send(value)
		L.Push(lua.LBool(ok))
		return 1
	}

	// Create and yield the operation
	L.Yield(&chanOperation{opType: chanSend, ch: ch, value: value})
	return -1
}

// receive implements channel:receive()
func (m *Module) receive(L *lua.LState) int {
	ch := L.CheckUserData(1).Value.(*Channel)

	// Try to receive immediately first
	if value, ok := ch.receive(); ok {
		L.Push(value)
		L.Push(lua.LBool(true))
		return 2
	}

	// Channel is empty and closed
	if ch.closed {
		L.Push(lua.LNil)
		L.Push(lua.LBool(false))
		return 2
	}

	// Create and yield the operation
	L.Yield(&chanOperation{opType: chanReceive, ch: ch})
	return -1
}

// close implements channel:close()
func (m *Module) close(L *lua.LState) int {
	ch := L.CheckUserData(1).Value.(*Channel)

	if ch.IsExternal() {
		L.RaiseError("cannot close external channel")
		return 0
	}

	if ch.closed {
		L.RaiseError("attempt to close already closed channel")
		return 0
	}

	L.Yield(&chanOperation{opType: chanClose, ch: ch})
	return -1
}

// todo: this is temp, TODO: DELETE IT!
func (m *Module) newExternal(L *lua.LState) int {
	ch := newExternalChannel(L.CheckString(1))
	ud := L.NewUserData()
	ud.Value = ch

	L.SetMetatable(ud, L.GetTypeMetatable("channel"))
	L.Push(ud)

	return 1
}
