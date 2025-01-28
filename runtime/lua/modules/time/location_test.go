package time

import (
	"context"
	"testing"

	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

func TestLocation(t *testing.T) {
	logger := zap.NewNop()

	t.Run("location constants", func(t *testing.T) {
		mod := NewTimeModule()
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Name(), mod.Loader),
			engine.WithGlobalFunction("assert", assertLua),
		)
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(context.Background(), `
			local time = require("time")
			assert(time.utc ~= nil)
			assert(time.localtz ~= nil)
			assert(tostring(time.utc) == "UTC")
		`, "test")
		assert.NoError(t, err)
	})

	t.Run("load_location", func(t *testing.T) {
		testCases := []struct {
			name          string
			location      string
			expectError   bool
			errorContains string
		}{
			{
				name:     "load America/New_York",
				location: "America/New_York",
			},
			{
				name:     "load UTC",
				location: "UTC",
			},
			{
				name:          "invalid location",
				location:      "Invalid/Location",
				expectError:   true,
				errorContains: "unknown time zone",
			},
			{
				name:          "empty location",
				location:      "",
				expectError:   true,
				errorContains: "empty location name",
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				mod := NewTimeModule()
				vm, err := engine.NewVM(logger,
					engine.WithLoader(mod.Name(), mod.Loader),
					engine.WithGlobalFunction("assert", assertLua),
				)
				require.NoError(t, err)
				defer vm.Close()

				script := `
					local time = require("time")
					local loc, err = time.load_location("` + tc.location + `")
					return loc, err
				`

				err = vm.DoString(context.Background(), script, "test")

				if tc.expectError {
					errStr := vm.State().Get(-1).String()
					assert.Contains(t, errStr, tc.errorContains)
					assert.Equal(t, lua.LNil, vm.State().Get(-2))
					vm.State().Pop(2)
				} else {
					require.NoError(t, err)
					assert.NotNil(t, vm.State().Get(-2))
					assert.Equal(t, lua.LNil, vm.State().Get(-1))
					vm.State().Pop(2)
				}
			})
		}
	})

	t.Run("fixed_zone", func(t *testing.T) {
		mod := NewTimeModule()
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Name(), mod.Loader),
			engine.WithGlobalFunction("assert", assertLua),
		)
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(context.Background(), `
			local time = require("time")
			local loc = time.fixed_zone("TEST", 3600)  -- UTC+1
			assert(tostring(loc) == "TEST")
		`, "test")
		assert.NoError(t, err)
	})
}
