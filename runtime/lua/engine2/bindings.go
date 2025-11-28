package engine2

import (
	lua "github.com/yuin/gopher-lua"
)

// SelectCase wraps a channel case for select operations
type SelectCase struct {
	Kind    ChannelOpKind
	Channel *Channel
	Value   lua.LValue
}

func (s *SelectCase) String() string       { return "<select_case>" }
func (s *SelectCase) Type() lua.LValueType { return lua.LTUserData }

// BindChannelFunctions binds channel.new and channel methods to Lua.
func BindChannelFunctions(l *lua.LState, proc *Process) {
	// channel module
	channelMod := l.NewTable()
	l.SetGlobal("channel", channelMod)

	// channel.new(bufferSize)
	l.SetField(channelMod, "new", l.NewFunction(func(l *lua.LState) int {
		bufSize := l.OptInt(1, 0)
		ch := NewChannel(bufSize)

		ud := l.NewUserData()
		ud.Value = ch
		l.SetMetatable(ud, l.GetTypeMetatable("channel"))
		l.Push(ud)
		return 1
	}))

	// channel.select(cases...) - select over multiple channel operations
	l.SetField(channelMod, "select", l.NewFunction(func(l *lua.LState) int {
		nargs := l.GetTop()
		if nargs == 0 {
			l.RaiseError("select requires at least one case")
			return 0
		}

		// Build SelectOp from cases
		selectOp := &SelectOp{
			Task:  l,
			Cases: make([]*ChannelOp, 0, nargs),
		}

		for i := 1; i <= nargs; i++ {
			arg := l.Get(i)

			// Check for default (nil or false)
			if arg == lua.LNil || arg == lua.LFalse {
				selectOp.HasDefault = true
				continue
			}

			// Must be SelectCase userdata
			ud, ok := arg.(*lua.LUserData)
			if !ok {
				l.RaiseError("select case %d: expected case_send/case_receive result", i)
				return 0
			}
			sc, ok := ud.Value.(*SelectCase)
			if !ok {
				l.RaiseError("select case %d: expected case_send/case_receive result", i)
				return 0
			}

			selectOp.Cases = append(selectOp.Cases, &ChannelOp{
				Kind:     sc.Kind,
				Channel:  sc.Channel,
				Value:    sc.Value,
				Task:     l,
				SelectOp: selectOp,
			})
		}

		// Try each case to see if any can proceed immediately
		for idx, caseOp := range selectOp.Cases {
			var result *ChannelResult
			if caseOp.Kind == SendOp {
				result = caseOp.Channel.Send(l, caseOp.Value, selectOp)
			} else {
				result = caseOp.Channel.Receive(l, selectOp)
			}

			updates := result.GetUpdates()
			if !result.Yields || len(updates) > 0 {
				// This case can proceed
				l.Push(lua.LNumber(idx + 1)) // 1-indexed
				if caseOp.Kind == ReceiveOp && len(updates) > 0 {
					res := updates[0].GetResult()
					for _, v := range res {
						l.Push(v)
					}
					return 1 + len(res)
				}
				return 1
			}
		}

		// No case ready - check for default
		if selectOp.HasDefault {
			l.Push(lua.LNumber(0)) // 0 indicates default
			return 1
		}

		// Block - register all cases and yield
		result := &ChannelResult{
			Yields: true,
			Block:  make([]*Channel, 0, len(selectOp.Cases)),
		}
		for _, c := range selectOp.Cases {
			result.Block = append(result.Block, c.Channel)
		}
		l.Push(result)
		return -1
	}))

	// Register channel metatable
	mt := l.NewTypeMetatable("channel")
	l.SetField(mt, "__index", l.SetFuncs(l.NewTable(), map[string]lua.LGFunction{
		"send": func(l *lua.LState) int {
			ud := l.CheckUserData(1)
			ch := ud.Value.(*Channel)
			val := l.Get(2)

			result := ch.Send(l, val, nil)
			if result.Yields {
				l.Push(result)
				return -1
			}
			// Non-blocking success
			updates := result.GetUpdates()
			if len(updates) > 0 && updates[0].Error != nil {
				l.RaiseError("%s", updates[0].Error.Error())
				return 0
			}
			l.Push(lua.LTrue)
			return 1
		},
		"recv": func(l *lua.LState) int {
			ud := l.CheckUserData(1)
			ch := ud.Value.(*Channel)

			result := ch.Receive(l, nil)
			if result.Yields {
				l.Push(result)
				return -1
			}
			// Non-blocking - return value immediately
			updates := result.GetUpdates()
			if len(updates) > 0 {
				res := updates[0].GetResult()
				if len(res) > 0 {
					for _, v := range res {
						l.Push(v)
					}
					return len(res)
				}
			}
			l.Push(lua.LNil)
			l.Push(lua.LFalse)
			return 2
		},
		"close": func(l *lua.LState) int {
			ud := l.CheckUserData(1)
			ch := ud.Value.(*Channel)
			ch.Close()
			return 0
		},
		"case_send": func(l *lua.LState) int {
			ud := l.CheckUserData(1)
			ch := ud.Value.(*Channel)
			val := l.Get(2)

			sc := &SelectCase{
				Kind:    SendOp,
				Channel: ch,
				Value:   val,
			}
			caseUd := l.NewUserData()
			caseUd.Value = sc
			l.Push(caseUd)
			return 1
		},
		"case_receive": func(l *lua.LState) int {
			ud := l.CheckUserData(1)
			ch := ud.Value.(*Channel)

			sc := &SelectCase{
				Kind:    ReceiveOp,
				Channel: ch,
			}
			caseUd := l.NewUserData()
			caseUd.Value = sc
			l.Push(caseUd)
			return 1
		},
	}))
}

// BindSubscribeFunctions binds subscribe/unsubscribe functions to Lua.
func BindSubscribeFunctions(l *lua.LState, proc *Process) {
	// subscribe(topic, channel)
	l.SetGlobal("subscribe", l.NewFunction(func(l *lua.LState) int {
		topic := l.CheckString(1)
		ud := l.CheckUserData(2)
		ch := ud.Value.(*Channel)

		req := &SubscribeRequest{Topic: topic, Channel: ch}
		l.Push(req)
		return -1
	}))

	// unsubscribe(channel)
	l.SetGlobal("unsubscribe", l.NewFunction(func(l *lua.LState) int {
		ud := l.CheckUserData(1)
		ch := ud.Value.(*Channel)

		req := &UnsubscribeRequest{Channel: ch}
		l.Push(req)
		return -1
	}))
}
