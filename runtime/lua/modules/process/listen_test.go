// SPDX-License-Identifier: MPL-2.0

package process

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/go-lua/types/typ"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	luapayload "github.com/wippyai/runtime/runtime/lua/engine/payload"
	systempayload "github.com/wippyai/runtime/system/payload"
)

func TestTypedListenFiltersDecodedMapsAndRetainsSender(t *testing.T) {
	for _, message := range []bool{false, true} {
		t.Run(map[bool]string{false: "raw", true: "message"}[message], func(t *testing.T) {
			l := lua.NewState()
			defer l.Close()
			tc := systempayload.NewTranscoder()
			luapayload.Register(tc)
			luapayload.RegisterAllBasicFormats(tc)
			ctx := payload.WithTranscoder(ctxapi.NewRootContext(), tc)
			options := l.NewTable()
			options.RawSetString("message", lua.LBool(message))
			options.RawSetString("type", lua.NewLType(typ.NewRecord().Field("count", typ.Integer).Build()))
			handler, err := listenHandler(options)
			require.NoError(t, err)
			source := pid.PID{Node: "remote-node", Host: "supervisor", UniqID: "actor-17"}
			for _, bad := range []payload.Payload{
				payload.NewPayload(map[string]any{"count": "wrong"}, payload.Golang),
				payload.NewPayload(map[string]any{"other": 17}, payload.Golang),
				payload.NewPayload([]byte("{broken"), payload.JSON),
			} {
				require.Nil(t, handler(ctx, l, source, "request", []payload.Payload{bad}))
			}
			good := payload.NewPayload(map[string]any{"count": 17, "from": "forged-node:owner:actor"}, payload.Golang)
			result := handler(ctx, l, source, "request", []payload.Payload{good})
			require.NotNil(t, result)
			if message {
				msg := result.(*lua.LUserData).Value.(*Message)
				require.Equal(t, source, msg.From)
				require.Equal(t, "request", msg.Topic)
				result = msg.data
			}
			require.Equal(t, lua.LInteger(17), result.(*lua.LTable).RawGetString("count"))
		})
	}
}

func TestTypedListenDecodeFailureDoesNotBecomeOptionalNil(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	options := l.NewTable()
	options.RawSetString("type", lua.NewLType(typ.NewOptional(typ.String)))
	handler, err := listenHandler(options)
	require.NoError(t, err)
	require.Nil(t, handler(context.Background(), l, pid.PID{}, "request",
		[]payload.Payload{payload.NewPayload([]byte("{broken"), payload.JSON)}))
	require.Equal(t, lua.LNil, handler(context.Background(), l, pid.PID{}, "request",
		[]payload.Payload{payload.NewPayload(lua.LNil, payload.Lua)}))
}

func TestListenRejectsInvalidOptions(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	for _, test := range []struct {
		value lua.LValue
		key   string
	}{
		{lua.LString("Request"), "type"},
		{lua.LInteger(1), "message"},
		{lua.LTrue, "typo"},
	} {
		options := l.NewTable()
		options.RawSetString(test.key, test.value)
		_, err := listenHandler(options)
		require.Error(t, err)
	}
	_, err := listenHandler(lua.LTrue)
	require.Error(t, err)
}
