// SPDX-License-Identifier: MPL-2.0

package engine

import (
	"testing"

	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/logs"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestPrintUsesLuaStringConversion(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	core, output := observer.New(zap.InfoLevel)
	l.SetContext(logs.WithLogger(ctxapi.NewRootContext(), zap.New(core)))
	LoadModuleDef(l, PrintModule)
	require.NoError(t, l.DoString(`
		local plain = {}
		local custom = setmetatable({}, {__tostring = function() return "custom" end})
		print(true, false, nil, 42, custom, "tail")
		print(plain)
		expected_plain = tostring(plain)
	`))
	require.Len(t, output.All(), 2)
	require.Equal(t, "true false nil 42 custom tail", output.All()[0].Message)
	require.Equal(t, l.GetGlobal("expected_plain").String(), output.All()[1].Message)
	require.ErrorContains(t, l.DoString(`print(setmetatable({}, {__tostring = function() error("conversion failed") end}))`), "conversion failed")
}
