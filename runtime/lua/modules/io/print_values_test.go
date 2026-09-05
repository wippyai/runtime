// SPDX-License-Identifier: MPL-2.0

package io

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/service/terminal"
)

func TestPrintValuesUseLuaConversion(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	bindIO(l)
	var stdout, stderr bytes.Buffer
	ctx, _ := ctxapi.OpenFrameContext(ctxapi.NewRootContext())
	require.NoError(t, terminal.WithTerminalContext(ctx, terminal.NewTerminalContext(nil, &stdout, &stderr)))
	l.SetContext(ctx)
	require.NoError(t, l.DoString(`
		local custom = setmetatable({}, {__tostring = function() return "custom" end})
		assert(io.print(true, false, nil, 42, custom, "tail"))
		assert(io.eprint(true, false, nil, 42, custom, "tail"))
	`))
	require.Equal(t, "true\tfalse\tnil\t42\tcustom\ttail\n", stdout.String())
	require.Equal(t, stdout.String(), stderr.String())
}
