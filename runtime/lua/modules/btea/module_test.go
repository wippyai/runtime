package btea

import (
	"testing"

	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestBteaModule(t *testing.T) {
	logger := zap.NewNop()

	t.Run("module loads successfully", func(t *testing.T) {
		mod := NewBteaModule(logger)
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(nil, `
			local btea = require("btea")
			assert(type(btea) == "table", "btea module should be a table")	
		`, "test_load")
		require.NoError(t, err)
	})
}
