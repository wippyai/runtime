// SPDX-License-Identifier: MPL-2.0

package lua

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	glua "github.com/wippyai/go-lua"
	"github.com/wippyai/go-lua/compiler/parse"
	"github.com/wippyai/runtime/api/registry"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/code"
)

func TestEvalImportBytecodeLifecycle(t *testing.T) {
	cm := newEvalCodeManager(t)
	id := registry.NewID("test", "bytecode-sdk")
	proto := func(version string) *glua.FunctionProto {
		chunk, err := parse.Parse(strings.NewReader(librarySource(version)), id.String())
		require.NoError(t, err)
		p, err := glua.CompileWithOptions(chunk, id.String(), glua.CompileOptions{})
		require.NoError(t, err)
		return p
	}
	node := code.Node{ID: id, Kind: luaapi.LibraryBytecode}
	require.NoError(t, cm.AddNodeWithProto(context.Background(), node, nil, proto("1")))
	host := newEvalHost(cm)
	require.Equal(t, "1:hello eval", runVersion(t, host, id))
	require.Equal(t, "1:hello eval", runVersion(t, host, id))

	require.NoError(t, cm.UpdateNodeWithProto(context.Background(), node, nil, proto("2")))
	require.Equal(t, "2:hello eval", runVersion(t, host, id))
	require.NoError(t, cm.DeleteNode(context.Background(), id))
	_, err := runVersionErr(host, id)
	require.Error(t, err)
	require.NoError(t, cm.AddNodeWithProto(context.Background(), node, nil, proto("3")))
	require.Equal(t, "3:hello eval", runVersion(t, host, id))
}
