// SPDX-License-Identifier: MPL-2.0

package process

import (
	"context"
	"fmt"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/runtime/lua/engine"
)

// listenHandler binds a receiver-local type. Neither the topic nor a type name
// supplied by a sender is evidence that its payload satisfies this type.
func listenHandler(options lua.LValue) (engine.TopicHandler, error) {
	if options == lua.LNil {
		return nil, nil
	}
	table, ok := options.(*lua.LTable)
	if !ok {
		return nil, fmt.Errorf("process.listen options must be a table")
	}
	var optionErr error
	table.ForEach(func(key, _ lua.LValue) {
		if key != lua.LString("message") && key != lua.LString("type") {
			optionErr = fmt.Errorf("unsupported process.listen option %s", key.String())
		}
	})
	if optionErr != nil {
		return nil, optionErr
	}
	message := table.RawGetString("message")
	if message != lua.LNil && message.Type() != lua.LTBool {
		return nil, fmt.Errorf("process.listen message option must be a boolean")
	}
	requested := table.RawGetString("type")
	if requested == lua.LNil {
		if message == lua.LTrue {
			return MessageHandler, nil
		}
		return nil, nil
	}
	expected, ok := requested.(*lua.LType)
	if !ok {
		return nil, fmt.Errorf("process.listen type option must be a type value")
	}
	return func(ctx context.Context, state *lua.LState, source pid.PID, topic string, inputs []payload.Payload) lua.LValue {
		// Decode explicitly: a decoding failure must not masquerade as nil and pass
		// a listener whose declared type is optional.
		values := make([]payload.Payload, len(inputs))
		for i, input := range inputs {
			if input == nil {
				return nil
			}
			if input.Format() != payload.Lua {
				transcoder := payload.GetTranscoder(ctx)
				if transcoder == nil {
					return nil
				}
				var err error
				input, err = transcoder.Transcode(input, payload.Lua)
				if err != nil || input == nil {
					return nil
				}
			}
			if _, ok := input.Data().(lua.LValue); !ok {
				return nil
			}
			values[i] = input
		}
		data := engine.PayloadsToLua(ctx, state, values)
		if !expected.Validate(state, data) {
			return nil
		}
		if message == lua.LTrue {
			msg := NewMessage(source, topic, inputs)
			msg.data = data
			return WrapMessage(state, msg)
		}
		return data
	}, nil
}
