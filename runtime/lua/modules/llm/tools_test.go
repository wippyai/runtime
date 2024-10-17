package llm_test

import (
	"testing"

	"git.spiralscout.com/estimation-engine/go-lua"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/stretchr/testify/require"
)

func TestParseToLuaTable(t *testing.T) {
	require.NotPanics(t, func() {
		data := map[string]any{
			"foo": "bar",
			"baz": 123,
			"qux": []string{"quux", "corge"},
			"a":   true,
		}

		l := lua.NewState()
		table := l.NewTable()
		for _, v := range data {
			table.Append(engine.GoToLua(l, v))
		}
	})
}
