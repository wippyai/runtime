package time

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/runtime/lua/engine"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

func assertLua(l *lua.LState) int {
	if l.ToBool(1) {
		return 0
	}
	l.RaiseError("%s", l.OptString(2, "assertion failed!"))
	return 0
}

func TestDuration(t *testing.T) {
	logger := zap.NewNop()

	t.Run("module creation and loading", func(t *testing.T) {
		mod := NewTimeModule()
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Info().Name, mod.Loader),
			engine.WithGlobalFunction("assert", assertLua),
		)
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(context.Background(), `
			local time = require("time")
			assert(type(time) == "table")
			assert(time.NANOSECOND == 1)
			assert(time.MICROSECOND == 1000)
			assert(time.MILLISECOND == 1000000)
			assert(time.SECOND == 1000000000)
			assert(time.MINUTE == 60000000000)
			assert(time.HOUR == 3600000000000)
		`, "test")
		assert.NoError(t, err)
	})

	t.Run("duration methods", func(t *testing.T) {
		testCases := []struct {
			name     string
			script   string
			expected float64
		}{
			{
				name: "nanoseconds",
				script: `
					local time = require("time")
					local d = time.parse_duration("1s")
					return d:nanoseconds()
				`,
				expected: 1000000000, // 1 second in nanoseconds
			},
			{
				name: "microseconds",
				script: `
					local time = require("time")
					local d = time.parse_duration("1s")
					return d:microseconds()
				`,
				expected: 1000000, // 1 second in microseconds
			},
			{
				name: "milliseconds",
				script: `
					local time = require("time")
					local d = time.parse_duration("1s")
					return d:milliseconds()
				`,
				expected: 1000, // 1 second in milliseconds
			},
			{
				name: "seconds",
				script: `
					local time = require("time")
					local d = time.parse_duration("1s")
					return d:seconds()
				`,
				expected: 1, // 1 second
			},
			{
				name: "minutes",
				script: `
					local time = require("time")
					local d = time.parse_duration("1h")
					return d:minutes()
				`,
				expected: 60, // 1 hour in minutes
			},
			{
				name: "hours",
				script: `
					local time = require("time")
					local d = time.parse_duration("2h")
					return d:hours()
				`,
				expected: 2, // 2 hours
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				mod := NewTimeModule()
				vm, err := engine.NewVM(logger,
					engine.WithLoader(mod.Info().Name, mod.Loader),
					engine.WithGlobalFunction("assert", assertLua),
				)
				require.NoError(t, err)
				defer vm.Close()

				err = vm.DoString(context.Background(), tc.script, "test")
				require.NoError(t, err)

				result := vm.State().Get(-1)
				assert.Equal(t, tc.expected, float64(result.(lua.LNumber)))
				vm.State().Pop(1)
			})
		}
	})

	t.Run("duration parsing", func(t *testing.T) {
		testCases := []struct {
			name     string
			duration string
			expected float64
		}{
			{
				name:     "parse nanoseconds",
				duration: "1ns",
				expected: 1,
			},
			{
				name:     "parse microseconds",
				duration: "1us",
				expected: 1000,
			},
			{
				name:     "parse milliseconds",
				duration: "1ms",
				expected: 1000000,
			},
			{
				name:     "parse seconds",
				duration: "1s",
				expected: 1000000000,
			},
			{
				name:     "parse minutes",
				duration: "1m",
				expected: 60000000000,
			},
			{
				name:     "parse hours",
				duration: "1h",
				expected: 3600000000000,
			},
			{
				name:     "parse composite duration",
				duration: "1h30m",
				expected: 5400000000000,
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				mod := NewTimeModule()
				vm, err := engine.NewVM(logger,
					engine.WithLoader(mod.Info().Name, mod.Loader),
					engine.WithGlobalFunction("assert", assertLua),
				)
				require.NoError(t, err)
				defer vm.Close()

				script := `
					local time = require("time")
					local d = time.parse_duration("` + tc.duration + `")
					return d:nanoseconds()
				`

				err = vm.DoString(context.Background(), script, "test")
				require.NoError(t, err)

				result := vm.State().Get(-1)
				assert.Equal(t, tc.expected, float64(result.(lua.LNumber)))
				vm.State().Pop(1)
			})
		}
	})

	t.Run("duration string representation", func(t *testing.T) {
		testCases := []struct {
			name     string
			duration string
			expected string
		}{
			{
				name:     "hours",
				duration: "2h",
				expected: "2h0m0s",
			},
			{
				name:     "minutes",
				duration: "5m",
				expected: "5m0s",
			},
			{
				name:     "seconds",
				duration: "10s",
				expected: "10s",
			},
			{
				name:     "milliseconds",
				duration: "300ms",
				expected: "300ms",
			},
			{
				name:     "complex duration",
				duration: "2h45m30s",
				expected: "2h45m30s",
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				mod := NewTimeModule()
				vm, err := engine.NewVM(logger,
					engine.WithLoader(mod.Info().Name, mod.Loader),
					engine.WithGlobalFunction("assert", assertLua),
				)
				require.NoError(t, err)
				defer vm.Close()

				script := `
					local time = require("time")
					local d = time.parse_duration("` + tc.duration + `")
					return tostring(d)
				`

				err = vm.DoString(context.Background(), script, "test")
				require.NoError(t, err)

				result := vm.State().Get(-1).String()
				assert.Equal(t, tc.expected, result)
				vm.State().Pop(1)
			})
		}
	})

	t.Run("error cases", func(t *testing.T) {
		testCases := []struct {
			name          string
			script        string
			expectedError string
		}{
			{
				name: "invalid duration string",
				script: `
					local time = require("time")
					local d, err = time.parse_duration("invalid")
					return d, err and tostring(err) or nil
				`,
				expectedError: "time: invalid duration",
			},
			{
				name: "method call on non-duration",
				script: `
					local time = require("time")
					local d = {}
					local success, err = pcall(function() return d:nanoseconds() end)
					return success, err
				`,
				expectedError: "attempt to call a non-function object",
			},
			{
				name: "duration method on invalid userdata",
				script: `
					local time = require("time")
					local d = newproxy(true) -- Creates a userdata with empty metatable
					local success, err = pcall(function() return d:nanoseconds() end)
					return success, err
				`,
				expectedError: "attempt to index a non-table object(userdata)",
			},
		}

		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				mod := NewTimeModule()
				vm, err := engine.NewVM(logger,
					engine.WithLoader(mod.Info().Name, mod.Loader),
					engine.WithGlobalFunction("assert", assertLua),
				)
				require.NoError(t, err)
				defer vm.Close()

				err = vm.DoString(context.Background(), tc.script, "test")
				require.NoError(t, err)

				if tc.name == "invalid duration string" {
					errStr := vm.State().Get(-1).String()
					assert.Contains(t, errStr, tc.expectedError)
					vm.State().Pop(2) // pop both nil and error
				} else {
					success := vm.State().Get(-2).(lua.LBool)
					assert.False(t, bool(success))
					errStr := vm.State().Get(-1).String()
					assert.Contains(t, errStr, tc.expectedError)
					vm.State().Pop(2)
				}
			})
		}
	})
}
