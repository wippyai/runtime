package time

import (
	"testing"
	"time"

	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

func TestCoroutineWithTime(t *testing.T) {
	logger := zap.NewNop()

	t.Run("coroutine with time module", func(t *testing.T) {
		mod := NewTimeModule()
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Name(), mod.Loader),
			engine.WithGlobalFunction("assert", assertLua),
		)
		require.NoError(t, err)
		defer vm.Close()

		script := `
            local time = require("time")
            local results = {}
            local count = 0

            -- Create a producer coroutine that yields with time delays
            local producer = coroutine.create(function()
                for i = 1, 3 do
                    local duration = time.parse_duration("100ms")
                    time.sleep(duration)
                    coroutine.yield(i * 10)
                end
                return "done"
            end)

            -- Test coroutine.status
            assert(coroutine.status(producer) == "suspended", "Initial status should be suspended")

            -- Resume the coroutine multiple times and collect results
            while coroutine.status(producer) ~= "dead" do
                local success, value = coroutine.resume(producer)
                assert(success, "Task resume should succeed")
                if value ~= "done" then
                    count = count + 1
                    results[count] = value
                end
            end

            -- Return results for validation
            return count, results[1], results[2], results[3]
        `

		startTime := time.Now()
		err = vm.DoString(nil, script, "test")
		elapsed := time.Since(startTime)

		require.NoError(t, err)

		// Check count and values
		count := vm.State().Get(-4)
		value1 := vm.State().Get(-3)
		value2 := vm.State().Get(-2)
		value3 := vm.State().Get(-1)

		assert.Equal(t, lua.LNumber(3), count)
		assert.Equal(t, lua.LNumber(10), value1)
		assert.Equal(t, lua.LNumber(20), value2)
		assert.Equal(t, lua.LNumber(30), value3)

		// Verify timing (should be at least 300ms but not too much longer)
		assert.GreaterOrEqual(t, elapsed.Milliseconds(), int64(300))
		assert.LessOrEqual(t, elapsed.Milliseconds(), int64(600))

		vm.State().Pop(4)
	})

	t.Run("coroutine error handling", func(t *testing.T) {
		mod := NewTimeModule()
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Name(), mod.Loader),
			engine.WithGlobalFunction("assert", assertLua),
		)
		require.NoError(t, err)
		defer vm.Close()

		script := `
            local time = require("time")
            
            -- Create a coroutine that will error by passing invalid argument directly to sleep
            local bad_routine = coroutine.create(function()
                time.sleep("not a duration")
            end)

            -- Attempt to resume and capture error
            local success, error_msg = coroutine.resume(bad_routine)
            return success, tostring(error_msg)
        `

		err = vm.DoString(nil, script, "test")
		require.NoError(t, err)

		success := vm.State().Get(-2)
		errorMsg := vm.State().Get(-1)

		assert.Equal(t, lua.LBool(false), success)
		assert.Contains(t, errorMsg.String(), "duration expected")

		vm.State().Pop(2)
	})

	t.Run("coroutine yield across time operations", func(t *testing.T) {
		mod := NewTimeModule()
		vm, err := engine.NewVM(logger,
			engine.WithLoader(mod.Name(), mod.Loader),
			engine.WithGlobalFunction("assert", assertLua),
		)
		require.NoError(t, err)
		defer vm.Close()

		script := `
            local time = require("time")
            
            -- Create a coroutine that yields timestamps
            local timer = coroutine.create(function()
                local start = time.now()
                coroutine.yield(start:unix())
                
                time.sleep(time.parse_duration("100ms"))
                local middle = time.now()
                coroutine.yield(middle:unix())
                
                time.sleep(time.parse_duration("100ms"))
                local finish = time.now()
                return finish:unix()
            end)

            local timestamps = {}
            local count = 0

            -- Collect timestamps from the coroutine
            while coroutine.status(timer) ~= "dead" do
                local success, value = coroutine.resume(timer)
                assert(success, "Task resume should succeed")
                count = count + 1
                timestamps[count] = value
            end

            -- Verify we got increasing timestamps
            assert(timestamps[2] >= timestamps[1], "Second timestamp should be >= first")
            assert(timestamps[3] >= timestamps[2], "Third timestamp should be >= second")

            return count
        `

		err = vm.DoString(nil, script, "test")
		require.NoError(t, err)

		count := vm.State().Get(-1)
		assert.Equal(t, lua.LNumber(3), count)

		vm.State().Pop(1)
	})
}
