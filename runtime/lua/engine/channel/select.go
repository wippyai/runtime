package channel

import (
	lua "github.com/yuin/gopher-lua"
)

// selectCase represents a single case in a select operation
type selectCase struct {
	chValue *lua.LUserData // The channel to operate on
	dir     chanOp         // Direction: send or receive
	value   lua.LValue     // Value to send (for send cases)
}

func (sc *selectCase) Channel() *Channel {
	ch, ok := sc.chValue.Value.(*Channel)
	if !ok {
		return nil
	}

	return ch
}

// Ensure selectCase implements lua.LValue
func (sc *selectCase) String() string {
	if sc.dir == chanSend {
		return "channel.case_send"
	}
	return "channel.case_receive"
}

func (sc *selectCase) Type() lua.LValueType {
	return lua.LTUserData
}

// selectOperation represents a yielded select operation
type selectOperation struct {
	cases      []*selectCase
	hasDefault bool
}

// Make selectOperation implement lua.LValue so it can be yielded
func (s *selectOperation) String() string {
	return "channel.select"
}

func (s *selectOperation) Type() lua.LValueType {
	return lua.LTUserData
}

// selectResult represents the result of a select operation
type selectResult struct {
	chValue *lua.LUserData // Selected channel
	value   lua.LValue     // Received value (for receive cases)
	ok      bool           // Success flag (false for closed channel)
}

// caseSend implements channel:case_send(value)
func caseSend(L *lua.LState) int {
	chValue := L.CheckUserData(1)
	ch, ok := chValue.Value.(*Channel)
	if !ok {
		L.RaiseError("invalid channel")
		return 0
	}

	value := L.CheckAny(2)
	if ch.IsExternal() {
		L.RaiseError("cannot send to external channel")
		return 0
	}

	sc := &selectCase{chValue: chValue, dir: chanSend, value: value}

	L.Push(sc)
	return 1
}

// caseReceive implements channel:case_receive()
func caseReceive(L *lua.LState) int {
	chValue := L.CheckUserData(1)
	_, ok := chValue.Value.(*Channel)
	if !ok {
		L.RaiseError("invalid channel")
		return 0
	}

	sc := &selectCase{chValue: chValue, dir: chanReceive}

	L.Push(sc)
	return 1
}

func selectOp(L *lua.LState) int {
	// Validate first argument is table
	casesTable := L.CheckTable(1)
	hasDefault := L.OptBool(2, false)

	// Extract and validate cases from table
	var cases []*selectCase
	casesTable.ForEach(func(k, value lua.LValue) {
		if k == lua.LString("default") {
			if hasDefault {
				L.RaiseError("multiple default cases in select")
				return
			}
			hasDefault = true
			return
		}

		if sc, ok := value.(*selectCase); ok {
			// Validate channel is not closed for send operations
			if sc.dir == chanSend && sc.Channel().closed {
				L.RaiseError("attempt to send on closed channel")
				return
			}
			cases = append(cases, sc)
		} else {
			L.RaiseError("invalid select case: expected channel case")
		}
	})

	// Validate we have cases or default
	if len(cases) == 0 && !hasDefault {
		L.RaiseError("select with no cases and no default")
		return 0
	}

	// try immediate operations first
	for _, c := range cases {
		ch := c.Channel()
		switch c.dir {
		case chanSend:
			// For buffered channels, try to send immediately
			if ch.capacity > 0 && !ch.isFull() {
				if ok := ch.send(c.value); ok {
					L.Push(makeSelectResult(L, &selectResult{
						chValue: c.chValue,
						ok:      true,
					}))
					return 1
				}
			}

		case chanReceive:
			// Try to receive immediately
			if value, ok := ch.receive(); ok {
				L.Push(makeSelectResult(L, &selectResult{
					chValue: c.chValue,
					value:   value,
					ok:      true,
				}))
				return 1
			}

			// Handle closed channels immediately
			if ch.closed {
				L.Push(makeSelectResult(L, &selectResult{
					chValue: c.chValue,
					value:   lua.LNil,
					ok:      false,
				}))
				return 1
			}
		case chanClose:
			// never happens
		}
	}

	// If we have a default case and no operation was ready
	if hasDefault {
		L.Push(makeSelectResult(L, &selectResult{chValue: nil, ok: true}))
		return 1
	}

	// Create select operation for yielding
	op := &selectOperation{
		cases:      cases,
		hasDefault: hasDefault,
	}

	// Yield the select operation
	L.Yield(op)
	return -1
}

// Helper function to create a select result table
func makeSelectResult(L *lua.LState, result *selectResult) *lua.LTable {
	tab := L.NewTable()

	// Set channel if one was selected
	if result.chValue != nil {
		tab.RawSetString("channel", result.chValue)
	} else {
		tab.RawSetString("channel", lua.LNil)
	}

	// Set value if it exists (for receive cases)
	if result.value != nil {
		tab.RawSetString("value", result.value)
	}

	// Set ok flag
	tab.RawSetString("ok", lua.LBool(result.ok))

	return tab
}
