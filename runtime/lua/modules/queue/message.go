package queue

import (
	"fmt"

	queueapi "github.com/wippyai/runtime/api/queue"
	lua "github.com/yuin/gopher-lua"
)

// Message represents a Lua userdata object wrapping queue.Message
type Message struct {
	message *queueapi.Message
}

// checkMessage gets and verifies Message userdata from Lua state
func checkMessage(l *lua.LState, n int) (*Message, error) {
	ud := l.CheckUserData(n)
	if ud == nil {
		return nil, fmt.Errorf("argument %d must be a Message", n)
	}

	if msg, ok := ud.Value.(*Message); ok {
		return msg, nil
	}
	return nil, fmt.Errorf("argument %d must be a Message, got %T", n, ud.Value)
}

// messageToString returns a string representation of the message
func messageToString(l *lua.LState) int {
	msg, err := checkMessage(l, 1)
	if err != nil {
		l.ArgError(1, err.Error())
		return 0
	}

	l.Push(lua.LString(fmt.Sprintf("QueueMessage{id=%s}", msg.message.ID)))
	return 1
}

// messageID returns the message ID
func messageID(l *lua.LState) int {
	msg, err := checkMessage(l, 1)
	if err != nil {
		l.ArgError(1, err.Error())
		return 0
	}

	l.Push(lua.LString(msg.message.ID))
	l.Push(lua.LNil)
	return 2
}

// messageHeader gets a specific message header value
func messageHeader(l *lua.LState) int {
	msg, err := checkMessage(l, 1)
	if err != nil {
		l.ArgError(1, err.Error())
		return 0
	}

	// Get header key (second argument)
	if l.GetTop() < 2 {
		l.Push(lua.LNil)
		l.Push(lua.LString("header key required"))
		return 2
	}

	key := l.CheckString(2)

	// Get header value from message headers
	if msg.message.Headers == nil {
		l.Push(lua.LNil)
		l.Push(lua.LNil)
		return 2
	}

	// Try to get the header value
	val, ok := msg.message.Headers.Get(key)
	if !ok {
		l.Push(lua.LNil)
		l.Push(lua.LNil)
		return 2
	}

	// Convert value to Lua type
	switch v := val.(type) {
	case string:
		l.Push(lua.LString(v))
	case int:
		l.Push(lua.LNumber(v))
	case int64:
		l.Push(lua.LNumber(v))
	case float64:
		l.Push(lua.LNumber(v))
	case bool:
		l.Push(lua.LBool(v))
	default:
		l.Push(lua.LString(fmt.Sprintf("%v", v)))
	}

	l.Push(lua.LNil)
	return 2
}

// messageHeaders returns all message headers as a Lua table
func messageHeaders(l *lua.LState) int {
	msg, err := checkMessage(l, 1)
	if err != nil {
		l.ArgError(1, err.Error())
		return 0
	}

	// Create headers table
	tbl := l.CreateTable(0, 10) // Pre-allocate for common headers

	if msg.message.Headers != nil {
		// Iterate over all headers (Bag is just a map[string]any)
		for key, value := range msg.message.Headers {
			// Convert value to Lua type
			switch v := value.(type) {
			case string:
				tbl.RawSetString(key, lua.LString(v))
			case int:
				tbl.RawSetString(key, lua.LNumber(v))
			case int64:
				tbl.RawSetString(key, lua.LNumber(v))
			case float64:
				tbl.RawSetString(key, lua.LNumber(v))
			case bool:
				tbl.RawSetString(key, lua.LBool(v))
			default:
				tbl.RawSetString(key, lua.LString(fmt.Sprintf("%v", v)))
			}
		}
	}

	l.Push(tbl)
	l.Push(lua.LNil)
	return 2
}
