// SPDX-License-Identifier: MPL-2.0

package lua

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	glua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/registry"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/code"
	"github.com/wippyai/runtime/runtime/lua/engine"
	"go.uber.org/zap"
)

func TestXMLComponentPreservesDeclaredImportAliases(t *testing.T) {
	ctx, _ := engineTestContext(t, zap.NewNop())
	loaded, err := Engine().Load(ctx)
	require.NoError(t, err)
	loaded, err = XML().Load(loaded)
	require.NoError(t, err)
	manager := GetCodeManager(loaded)
	require.NotNil(t, manager)

	// An application may already call its own library "xml". Registering a
	// built-in must preserve that import while allowing both to coexist.
	libraryID := registry.NewID("app", "xml")
	require.NoError(t, manager.AddNode(context.Background(), code.Node{
		ID:     libraryID,
		Kind:   luaapi.Library,
		Source: `return { marker = "application-xml" }`,
	}, nil))
	functionID := registry.NewID("app", "xml_consumer")
	require.NoError(t, manager.AddNode(context.Background(), code.Node{
		ID:     functionID,
		Kind:   luaapi.Function,
		Source: `return function() return xml.marker end`,
		Method: "main",
	}, []code.Import{
		{ID: libraryID, Alias: "xml"},
		{ID: registry.NewID("", "xml"), Alias: "native_xml"},
	}))
	factory, err := engine.NewProcessFactory(manager).CreateFactory(functionID)
	require.NoError(t, err)
	process, err := factory()
	require.NoError(t, err)
	t.Cleanup(process.Close)
	state := process.(*engine.Process).State()

	applicationXML := state.GetField(state.Env, "xml")
	require.IsType(t, &glua.LTable{}, applicationXML)
	require.Equal(t, glua.LString("application-xml"), applicationXML.(*glua.LTable).RawGetString("marker"))
	nativeXML := state.GetField(state.Env, "native_xml")
	require.IsType(t, &glua.LTable{}, nativeXML)
	require.Equal(t, glua.LTFunction, nativeXML.(*glua.LTable).RawGetString("decode").Type())

	for name, want := range map[string]glua.LValue{"xml": applicationXML, "native_xml": nativeXML} {
		require.NoError(t, state.CallByParam(glua.P{
			Fn: state.Env.RawGetString("require").(*glua.LFunction), NRet: 1, Protect: true,
		}, glua.LString(name)))
		require.Same(t, want, state.Get(-1))
		state.Pop(1)
	}
}
