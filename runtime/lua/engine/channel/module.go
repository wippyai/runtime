package channel

import (
	"fmt"

	lua "github.com/yuin/gopher-lua"
)

// Lua interface for op
func (o *op) String() string {
	return fmt.Sprintf("channel.op{kind=%d}", o.kind)
}

func (o *op) Type() lua.LValueType {
	return lua.LTChannel
}

// Lua interface for onNext
func (m *onNext) String() string {
	if len(m.block) > 0 {
		return fmt.Sprintf("next{yields=true,block=%v}", m.block)
	}

	return fmt.Sprintf("next{yields=%t}", m.yields)
}

func (m *onNext) Type() lua.LValueType {
	return lua.LTChannel
}

// Module represents a channel Lua module
type Module struct{}

// NewChannelModule creates a new channel module instance
func NewChannelModule() *Module {
	return &Module{}
}

// Name returns the module name
func (m *Module) Name() string {
	return "channel"
}

// Loader registers the module functions
func (m *Module) Loader(L *lua.LState) int {
	// Create module table
	mod := L.NewTable()

	// Register constructors
	L.SetField(mod, "new", L.NewFunction(newChannelLua))
	L.SetField(mod, "named", L.NewFunction(newNamedChannelLua))
	L.SetField(mod, "select", L.NewFunction(selectLua))

	// Channel methods
	channelMethods := map[string]lua.LGFunction{
		"send":             sendLua,
		"receive":          receiveLua,
		"close":            closeLua,
		"case_send":        caseSendLua,
		"case_receive":     caseReceiveLua,
		"_debug_size":      debugSizeLua,
		"_debug_senders":   debugSendersLua,
		"_debug_receivers": debugReceiversLua,
	}

	// Channel metatable
	mt := L.NewTypeMetatable("channel")
	L.SetField(mt, "__index", L.SetFuncs(L.NewTable(), channelMethods))

	// Register module
	L.Push(mod)
	return 1
}

// Constructor functions
func newChannelLua(L *lua.LState) int {
	capacity := L.OptInt(1, 0)
	if capacity < 0 {
		L.RaiseError("channel capacity must be >= 0")
		return 0
	}

	ch := newChannel(capacity)
	ud := L.NewUserData()
	ud.Value = ch
	ch.value = ud // yep
	L.SetMetatable(ud, L.GetTypeMetatable("channel"))
	L.Push(ud)
	return 1
}

func newNamedChannelLua(L *lua.LState) int {
	name := L.CheckString(1)
	capacity := L.OptInt(2, 0)
	if capacity < 0 {
		L.RaiseError("channel capacity must be >= 0")
		return 0
	}

	ch := Named(name, capacity)
	ud := L.NewUserData()
	ud.Value = ch
	ch.value = ud // yep
	L.SetMetatable(ud, L.GetTypeMetatable("channel"))
	L.Push(ud)
	return 1
}

// Channel methods
func sendLua(L *lua.LState) int {
	ch := checkChannel(L)
	value := L.CheckAny(2)

	if ch.isNamed() {
		L.RaiseError("cannot send to named channel")
		return 0
	}

	if ch.closed {
		L.RaiseError("attempt to send on closed channel")
		return 0
	}

	next := ch.send(L, value, nil)

	if next.yields {
		L.Push(next)
		return -1
	}

	if len(next.next) > 0 && next.next[0].err != nil {
		L.RaiseError(next.next[0].err.Error())
		return 0
	}

	L.Push(lua.LBool(true))
	return 1
}

func receiveLua(L *lua.LState) int {
	ch := checkChannel(L)
	next := ch.receive(L, nil)

	if next.yields {
		L.Push(next)
		return -1 // yield to scheduler
	}

	if len(next.next) > 0 {
		result := next.next[0]
		if result.err != nil {
			L.RaiseError(result.err.Error())
			return 0
		}

		if len(result.values) == 2 {
			L.Push(result.values[0]) // value
			L.Push(result.values[1]) // ok
			return 2
		}
	}

	L.RaiseError("invalid receive result")
	return 0
}

func closeLua(L *lua.LState) int {
	ch := checkChannel(L)

	if ch.isNamed() {
		L.RaiseError("cannot close named channel")
		return 0
	}

	if ch.closed {
		L.RaiseError("attempt to close already closed channel")
		return 0
	}

	next := ch.close(L)

	if next.yields {
		L.Push(next)
		return -1 // yield to scheduler
	}

	// Handle immediate next
	if len(next.next) > 0 && next.next[0].err != nil {
		L.RaiseError(next.next[0].err.Error())
		return 0
	}

	return 0
}

// Select case functions
func caseSendLua(L *lua.LState) int {
	ch := checkChannel(L)
	value := L.CheckAny(2)

	// Check for invalid send operations
	if ch.isNamed() {
		L.RaiseError("cannot send to named channel")
		return 0
	}

	L.Push(&op{kind: sendOp, ch: ch, value: value})
	return 1
}

func caseReceiveLua(L *lua.LState) int {
	ch := checkChannel(L)

	L.Push(&op{kind: receiveOp, ch: ch})
	return 1
}

// Select function
func selectLua(L *lua.LState) int {
	// Check if the first argument is a table
	casesTable := L.CheckTable(1)
	hasDefault := L.OptBool(2, false)

	var cases []*op
	casesTable.ForEach(func(key, value lua.LValue) {
		if key.Type() == lua.LTString && key.String() == "default" {
			if v, ok := value.(lua.LBool); ok && bool(v) {
				hasDefault = true
			}
		} else if caseOp, ok := value.(*op); ok {
			cases = append(cases, caseOp)
		} else {
			L.RaiseError("Invalid select case")
		}
	})

	// Create a new select operation
	selectOp := &selectOp{
		cases:      cases,
		hasDefault: hasDefault,
		task:       L,
	}

	// Try to execute the select operation
	next := trySelect(L, selectOp)
	if next.yields {
		L.Push(next)
		return -1
	}

	if len(next.next) > 0 {
		result := next.next[0]
		if result.err != nil {
			L.RaiseError(result.err.Error())
			return 0
		}
		if len(result.values) > 0 {
			L.Push(result.values[0])
			return 1
		}
	}

	L.RaiseError("invalid select result")
	return 0
}

// trySelects checks the ability of immediate select operation
func trySelect(L *lua.LState, selectOp *selectOp) *onNext {
	waits := make([]*Channel, 0, len(selectOp.cases))
	for _, caseOp := range selectOp.cases {
		caseOp.selectOp = selectOp // for future reference
		waits = append(waits, caseOp.ch)

		switch caseOp.kind {
		case sendOp:
			m := caseOp.ch.send(L, caseOp.value, selectOp)
			if !m.yields {
				flushSelects(selectOp)
				return m
			}
		case receiveOp:
			m := caseOp.ch.receive(L, selectOp)
			if !m.yields {
				flushSelects(selectOp)
				return m
			}
		}
	}

	// Handle default case
	if selectOp.hasDefault {
		flushSelects(selectOp)

		result := L.NewTable()
		result.RawSetString("default", lua.LBool(true))
		result.RawSetString("ok", lua.LBool(true))
		return &onNext{
			next: []*opStep{
				{task: L, values: []lua.LValue{result}},
			},
		}
	}

	// Must block
	return &onNext{yields: true, block: waits}
}

// Helper functions
func checkChannel(L *lua.LState) *Channel {
	ud := L.CheckUserData(1)
	if ch, ok := ud.Value.(*Channel); ok {
		return ch
	}
	L.ArgError(1, "channel expected")
	return nil
}

// Debug methods
func debugSizeLua(L *lua.LState) int {
	ch := checkChannel(L)
	L.Push(lua.LNumber(ch.size))
	return 1
}

func debugSendersLua(L *lua.LState) int {
	ch := checkChannel(L)
	L.Push(lua.LNumber(ch.senders.Len()))
	return 1
}

func debugReceiversLua(L *lua.LState) int {
	ch := checkChannel(L)
	L.Push(lua.LNumber(ch.receivers.Len()))
	return 1
}
