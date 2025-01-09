package channel

import (
	lua "github.com/yuin/gopher-lua"
)

// Select Operations

// selectCase represents a single case in a select operation.
type selectCase struct {
	chValue *lua.LUserData // The channel to operate on.
	dir     chanOp         // Direction: send or receive.
	value   lua.LValue     // Value to send (for send cases).
}

// Channel retrieves the Channel from the selectCase.
func (sc *selectCase) Channel() *Channel {
	ch, ok := sc.chValue.Value.(*Channel)
	if !ok {
		return nil
	}
	return ch
}

// String returns a string representation of the selectCase.
func (sc *selectCase) String() string {
	if sc.dir == chanSend {
		return "channel.case_send"
	}
	return "channel.case_receive"
}

// Type returns the Lua type of the selectCase.
func (sc *selectCase) Type() lua.LValueType {
	return lua.LTUserData
}

// selectOperation represents a yielded select operation.
type selectOperation struct {
	cases      []*selectCase
	hasDefault bool
}

// String returns a string representation of the selectOperation.
func (s *selectOperation) String() string {
	return "channel.select"
}

// Type returns the Lua type of the selectOperation.
func (s *selectOperation) Type() lua.LValueType {
	return lua.LTUserData
}

// caseResult creates a select result from a yielded selectOperation.
func (s *selectOperation) caseResult(l *lua.LState, ch *Channel, value lua.LValue, ok bool) lua.LValue {
	for _, sc := range s.cases {
		if sc.Channel() == ch {
			return makeSelectResult(l, &selectResult{
				chValue: sc.chValue,
				value:   value,
				ok:      ok,
			})
		}
	}
	return nil
}

// selectResult represents the result of a select operation.
type selectResult struct {
	chValue *lua.LUserData // Selected channel.
	value   lua.LValue     // Received value (for receive cases).
	ok      bool           // Success flag (false for closed channel).
}

// makeSelectResult creates a Lua table representing the selectResult.
func makeSelectResult(L *lua.LState, result *selectResult) *lua.LTable {
	tab := L.NewTable()

	if result.chValue != nil {
		tab.RawSetString("channel", result.chValue)
	} else {
		tab.RawSetString("channel", lua.LNil)
	}

	if result.value != nil {
		tab.RawSetString("value", result.value)
	}

	tab.RawSetString("ok", lua.LBool(result.ok))
	return tab
}

// Lua API Functions

// caseSend implements channel:case_send(value) for use in select.
func caseSend(L *lua.LState) int {
	chValue := L.CheckUserData(1)
	ch, ok := chValue.Value.(*Channel)
	if !ok {
		L.RaiseError("invalid channel")
	}

	value := L.CheckAny(2)
	if ch.IsNamed() {
		L.RaiseError("cannot send to inbox channel")
	}

	sc := &selectCase{chValue: chValue, dir: chanSend, value: value}
	L.Push(sc)
	return 1
}

// caseReceive implements channel:case_receive() for use in select.
func caseReceive(L *lua.LState) int {
	chValue := L.CheckUserData(1)
	if _, ok := chValue.Value.(*Channel); !ok {
		L.RaiseError("invalid channel")
	}

	sc := &selectCase{chValue: chValue, dir: chanReceive}
	L.Push(sc)
	return 1
}

// selectOp implements the select operation.
func selectOp(L *lua.LState) int {
	casesTable := L.CheckTable(1)
	hasDefault := L.OptBool(2, false)

	var cases []*selectCase
	casesTable.ForEach(func(k, value lua.LValue) {
		if k.String() == "default" {
			if hasDefault {
				L.RaiseError("multiple default cases in select")
			}
			hasDefault = true
			return // Continue ForEach loop.
		}

		sc, ok := value.(*selectCase)
		if !ok {
			L.RaiseError("invalid select case: expected channel case")
		}

		if sc.dir == chanSend && sc.Channel().closed {
			L.RaiseError("attempt to send on closed channel")
		}

		cases = append(cases, sc)
	})

	if len(cases) == 0 && !hasDefault {
		L.RaiseError("select with no cases and no default")
	}

	// Try immediate operations first.
	for _, c := range cases {
		ch := c.Channel()
		switch c.dir {
		case chanSend:
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
			value, ok := ch.receive()
			if ok {
				L.Push(makeSelectResult(L, &selectResult{
					chValue: c.chValue,
					value:   value,
					ok:      true,
				}))
				return 1
			}

			if ch.closed {
				L.Push(makeSelectResult(L, &selectResult{
					chValue: c.chValue,
					value:   lua.LNil,
					ok:      false,
				}))
				return 1
			}
		}
	}

	// Handle default case if no operation was ready.
	if hasDefault {
		L.Push(makeSelectResult(L, &selectResult{chValue: nil, ok: true}))
		return 1
	}

	// Yield the select operation.
	op := &selectOperation{cases: cases, hasDefault: hasDefault}
	L.Yield(op)
	return -1
}
