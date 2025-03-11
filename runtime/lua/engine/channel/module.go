package channel

import (
	"fmt"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/value"
	"strings"

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
	var parts []string

	// Basic State
	parts = append(parts, fmt.Sprintf("yields=%t", m.yields))

	// Next operations
	if len(m.next) > 0 {
		opDetails := make([]string, 0, len(m.next))
		for i, op := range m.next {
			details := []string{fmt.Sprintf("op%d{", i)}

			// State info
			if op.State != nil {
				details = append(details, "hasState=true")
			}

			// Error info
			if op.Error != nil {
				details = append(details, fmt.Sprintf("Error='%s'", op.Error))
			}

			// Values info
			if len(op.Result) > 0 {
				valueStrs := make([]string, 0, len(op.Result))
				for _, v := range op.Result {
					if v == nil {
						valueStrs = append(valueStrs, "nil")
					} else {
						valueStrs = append(valueStrs, v.String())
					}
				}
				details = append(details, fmt.Sprintf("Result=[%s]", strings.Join(valueStrs, ",")))
			}

			opDetails = append(opDetails, strings.Join(details, " ")+"}")
		}
		parts = append(parts, fmt.Sprintf("next=[%s]", strings.Join(opDetails, " ")))
	}

	// Blocked channels
	if len(m.block) > 0 {
		chDetails := make([]string, 0, len(m.block))
		for _, ch := range m.block {
			details := []string{
				fmt.Sprintf("addr=%p", ch),
			}
			if ch.name != "" {
				details = append(details, fmt.Sprintf("name='%s'", ch.name))
			}
			details = append(details,
				fmt.Sprintf("cap=%d", ch.capacity),
				fmt.Sprintf("size=%d", ch.size),
				fmt.Sprintf("closed=%t", ch.closed),
				fmt.Sprintf("senders=%d", ch.senders.Len()),
				fmt.Sprintf("receivers=%d", ch.receivers.Len()),
			)
			chDetails = append(chDetails, fmt.Sprintf("{%s}", strings.Join(details, " ")))
		}
		parts = append(parts, fmt.Sprintf("block=[%s]", strings.Join(chDetails, " ")))
	}

	// Release channels
	if len(m.release) > 0 {
		chDetails := make([]string, 0, len(m.release))
		for _, ch := range m.release {
			details := []string{
				fmt.Sprintf("addr=%p", ch),
			}
			if ch.name != "" {
				details = append(details, fmt.Sprintf("name='%s'", ch.name))
			}
			details = append(details,
				fmt.Sprintf("cap=%d", ch.capacity),
				fmt.Sprintf("size=%d", ch.size),
				fmt.Sprintf("closed=%t", ch.closed),
				fmt.Sprintf("senders=%d", ch.senders.Len()),
				fmt.Sprintf("receivers=%d", ch.receivers.Len()),
			)
			chDetails = append(chDetails, fmt.Sprintf("{%s}", strings.Join(details, " ")))
		}
		parts = append(parts, fmt.Sprintf("release=[%s]", strings.Join(chDetails, " ")))
	}

	return fmt.Sprintf("next{%s}", strings.Join(parts, " "))
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
func (m *Module) Loader(l *lua.LState) int {
	// Create module table with exact size
	mod := l.CreateTable(0, 2)
	mod.RawSetString("new", l.NewFunction(newChannelLua))
	mod.RawSetString("select", l.NewFunction(selectLua))

	// Register all channel methods at once with the helper function
	value.RegisterMethods(l, "channel", map[string]lua.LGFunction{
		"send":             sendLua,
		"receive":          receiveLua,
		"close":            closeLua,
		"case_send":        caseSendLua,
		"case_receive":     caseReceiveLua,
		"_debug_size":      debugSizeLua,
		"_debug_senders":   debugSendersLua,
		"_debug_receivers": debugReceiversLua,
	})

	// Push module table to stack
	l.Push(mod)
	return 1
}

// Wrap wraps a channel into a Lua value.
func Wrap(l *lua.LState, ch *Channel) lua.LValue {
	ud := l.NewUserData()
	ud.Value = ch
	ud.Metatable = value.GetTypeMetatable(l, "channel")
	ch.value = ud // for select conditions they are always coupled

	return ud
}

// Constructor functions
func newChannelLua(l *lua.LState) int {
	capacity := l.OptInt(1, 0)
	if capacity < 0 {
		l.RaiseError("channel capacity must be >= 0")
		return 0
	}

	ch := newChannel(capacity)

	ud := l.NewUserData()
	ud.Value = ch
	ud.Metatable = value.GetTypeMetatable(l, "channel")
	ch.value = ud // yep

	l.Push(ud)
	return 1
}

// Channel methods
func sendLua(l *lua.LState) int {
	ch := CheckChannel(l)
	v := l.CheckAny(2)

	if ch.isNamed() {
		l.RaiseError("cannot send to named channel")
		return 0
	}

	if ch.closed {
		l.RaiseError("attempt to send on closed channel")
		return 0
	}

	next := ch.send(l, v, nil)

	if next.yields {
		l.Push(next)
		return -1
	}

	if len(next.next) > 0 && next.next[0].Error != nil {
		l.RaiseError("%s", next.next[0].Error.Error())
		return 0
	}

	l.Push(lua.LBool(true))
	return 1
}

func receiveLua(l *lua.LState) int {
	ch := CheckChannel(l)

	return Receive(l, ch)
}

func Receive(l *lua.LState, ch *Channel) int {
	next := ch.receive(l, nil)

	if next.yields {
		l.Push(next)
		return -1 // yield to scheduler
	}

	if len(next.next) > 0 {
		result := next.next[0]
		if result.Error != nil {
			l.RaiseError("%s", result.Error.Error())
			return 0
		}

		if len(result.Result) == 2 {
			l.Push(result.Result[0]) // value
			l.Push(result.Result[1]) // ok
			return 2
		}
	}

	l.RaiseError("invalid receive result")
	return 0
}

func closeLua(l *lua.LState) int {
	ch := CheckChannel(l)

	if ch.isNamed() {
		l.RaiseError("cannot close named channel")
		return 0
	}

	if ch.closed {
		l.RaiseError("attempt to close already closed channel")
		return 0
	}

	next := ch.close(l)

	if next.yields {
		l.Push(next)
		return -1 // yield to scheduler
	}

	// Handle immediate next
	if len(next.next) > 0 && next.next[0].Error != nil {
		l.RaiseError("%s", next.next[0].Error.Error())
		return 0
	}

	return 0
}

// Select case functions
func caseSendLua(l *lua.LState) int {
	ch := CheckChannel(l)
	v := l.CheckAny(2)

	// Check for invalid send operations
	if ch.isNamed() {
		l.RaiseError("cannot send to named channel")
		return 0
	}

	l.Push(&op{kind: sendOp, ch: ch, value: v})
	return 1
}

func caseReceiveLua(l *lua.LState) int {
	ch := CheckChannel(l)

	l.Push(&op{kind: receiveOp, ch: ch})
	return 1
}

// Select function
func selectLua(l *lua.LState) int {
	// Check if the first argument is a table
	casesTable := l.CheckTable(1)
	hasDefault := l.OptBool(2, false)

	var cases []*op
	casesTable.ForEach(func(key, value lua.LValue) {
		if key.Type() == lua.LTString && key.String() == "default" {
			if v, ok := value.(lua.LBool); ok && bool(v) {
				hasDefault = true
			}
		} else if caseOp, ok := value.(*op); ok {
			cases = append(cases, caseOp)
		} else {
			l.RaiseError("Invalid select case")
		}
	})

	// Spawn a new select operation
	selectOp := &selectOp{
		cases:      cases,
		hasDefault: hasDefault,
		task:       l,
	}

	// Try to execute the select operation
	next := trySelect(l, selectOp)
	if next.yields {
		l.Push(next)
		return -1
	}

	if len(next.next) > 0 {
		result := next.next[0]
		if result.Error != nil {
			l.RaiseError("%s", result.Error.Error())
			return 0
		}
		if len(result.Result) > 0 {
			l.Push(result.Result[0])
			return 1
		}
	}

	l.RaiseError("invalid select result")
	return 0
}

// trySelects checks the ability of immediate select operation
func trySelect(l *lua.LState, selectOp *selectOp) *onNext {
	// waits := make([]*Channel, 0, len(selectOp.cases))
	// next := make([]*Update, 0)
	nNext := &onNext{
		yields:  true,
		next:    make([]*engine.Update, 0),
		block:   make([]*Channel, 0),
		release: make([]*Channel, 0),
	}

	// check if we can execute chan operation immediately
	for _, caseOp := range selectOp.cases {
		switch caseOp.kind {
		case sendOp:
			if caseOp.ch.canSend() {
				return caseOp.ch.send(l, caseOp.value, selectOp)
			}
		case receiveOp:
			if caseOp.ch.canReceive() {
				return caseOp.ch.receive(l, selectOp)
			}
		}
	}

	// Handle default case
	if selectOp.hasDefault {
		result := l.CreateTable(0, 2)
		result.RawSetString("default", lua.LBool(true))
		result.RawSetString("ok", lua.LBool(true))

		return &onNext{
			next: []*engine.Update{
				{State: l, Result: []lua.LValue{result}},
			},
		}
	}

	for _, caseOp := range selectOp.cases {
		caseOp.selectOp = selectOp // for future reference

		switch caseOp.kind {
		case sendOp:
			m := caseOp.ch.send(l, caseOp.value, selectOp)

			// merge
			nNext.next = append(nNext.next, m.next...)
			nNext.block = append(nNext.block, m.block...)
			nNext.release = append(nNext.release, m.release...)
		case receiveOp:
			m := caseOp.ch.receive(l, selectOp)

			// merge
			nNext.next = append(nNext.next, m.next...)
			nNext.block = append(nNext.block, m.block...)
			nNext.release = append(nNext.release, m.release...)
		}
	}

	// Must block
	return nNext
}

// CheckChannel checks if the first argument is a channel and returns it.
func CheckChannel(l *lua.LState) *Channel {
	ud := l.CheckUserData(1)
	if ch, ok := ud.Value.(*Channel); ok {
		return ch
	}
	l.ArgError(1, "channel expected")
	return nil
}

// Debug methods
func debugSizeLua(l *lua.LState) int {
	ch := CheckChannel(l)
	l.Push(lua.LNumber(ch.size))
	return 1
}

func debugSendersLua(l *lua.LState) int {
	ch := CheckChannel(l)
	l.Push(lua.LNumber(ch.senders.Len()))
	return 1
}

func debugReceiversLua(l *lua.LState) int {
	ch := CheckChannel(l)
	l.Push(lua.LNumber(ch.receivers.Len()))
	return 1
}
