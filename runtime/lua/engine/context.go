package engine

import (
	"context"

	"github.com/wippyai/runtime/api/payload"
	lua "github.com/yuin/gopher-lua"
)

// TopicHandler processes incoming messages for a topic before channel delivery.
// Return value is what gets sent to the channel. Return nil to skip channel send.
type TopicHandler func(ctx context.Context, l *lua.LState, payloads []payload.Payload) lua.LValue
