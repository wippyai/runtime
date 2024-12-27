package treesitter

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/ponyruntime/pony/runtime/lua/engine"
	cjson "github.com/ponyruntime/pony/runtime/lua/modules/json"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestInitTS(t *testing.T) {
	zd := zap.NewNop()
	m := NewModule(zd)

	args := make(map[string]any, 2)
	// lua expects this file to be in the tests directory
	args["file_name"] = "tests/demo.php"
	zl := zap.NewNop()

	eng := engine.NewLuaEngine(context.Background(), zl)
	eng.L.PreloadModule("treesitter", m.Loader)
	eng.L.PreloadModule("json", cjson.Loader)
	eng.L.SetGlobal("args", engine.GoToLua(eng.L, args))

	data, err := os.ReadFile("./tests/ts.lua")
	require.NoError(t, err)

	err = eng.DoString(string(data), "<string>")
	require.NoError(t, err)

	result := engine.ToGoAny(eng.L.Get(-1))
	// we should not Pop values if there are no values on the Lua stack
	if eng.L.GetTop() != 0 {
		eng.Pop(1)
	}
	if result == nil {
		t.Fatal("result should not be nil")
	}

	fmt.Println(result)
}
