// SPDX-License-Identifier: MPL-2.0

package xml

import (
	"context"
	"io"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/runtime/resource"
	xmlapi "github.com/wippyai/runtime/api/xml"
	streammod "github.com/wippyai/runtime/runtime/lua/modules/stream"
	"github.com/wippyai/runtime/system/xml/xmlcmd"
)

// opContext returns the context of the running state; a state without one has no cancellation.
func opContext(l *lua.LState) context.Context {
	if ctx := l.Context(); ctx != nil {
		return ctx
	}
	return context.Background()
}

func tableOf(l *lua.LState) *resource.Table {
	if ctx := l.Context(); ctx != nil {
		return resource.GetTable(ctx)
	}
	return nil
}

// streamSource resolves a readable stream argument.
func streamSource(l *lua.LState, ud *lua.LUserData, table *resource.Table) (io.Reader, *lua.Error) {
	switch v := ud.Value.(type) {
	case *streammod.Stream:
		if table == nil {
			return nil, luaError(l, xmlapi.ErrNoTable)
		}
		src, err := xmlcmd.StreamSource(table, v.ID)
		if err != nil {
			return nil, argument(l, "source stream: "+err.Error())
		}
		return src, nil
	case io.Reader:
		if table == nil {
			return nil, luaError(l, xmlapi.ErrNoTable)
		}
		return v, nil
	default:
		return nil, argument(l, "source must be a string or a readable stream")
	}
}
