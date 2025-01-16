package async

import (
	"context"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
	"github.com/stretchr/testify/assert"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
	"testing"
	"time"
)

func TestRunner_AsLayer(t *testing.T) {
	// Setup VM
	log := zap.NewNop()
	vm, err := engine.NewCVM(log,
		engine.WithGlobalFunction("async_sleep", func(L *lua.LState) int {
			WrapAsync(L, func(args []lua.LValue) Result {
				time.Sleep(100 * time.Millisecond)
				return Result{Values: []lua.LValue{lua.LString("done-sleep")}}
			})
			return -1
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer vm.Close()

	// Create wrapped VM with runner layer
	wrapped := engine.NewWrappedCVM(vm)
	wrapped.AddLayer(NewAsyncRunner())

	// Import test script
	err = vm.Import(`
			function test_coroutine()
				local result = async_sleep(100)	
				return "done"
			end
		`, "test", "test_coroutine")

	if err != nil {
		t.Fatal(err)
	}

	// Execute through layer
	result, err := wrapped.Execute(context.Background(), "test_coroutine")
	if err != nil {
		t.Fatal(err)
	}

	if result.String() != "done" {
		t.Errorf("unexpected result: got %v, want 'done'", result)
	}
}
func TestAsyncCoroutines(t *testing.T) {
	t.Run("concurrent coroutines with different sleep durations", func(t *testing.T) {
		log := zap.NewNop()

		// Create base VM with sleep function
		vm, err := engine.NewCVM(log,
			engine.WithGlobalFunction("async_sleep", func(L *lua.LState) int {
				_ = L.CheckNumber(1) // Get sleep duration in ms
				WrapAsync(L, func(args []lua.LValue) Result {
					if len(args) < 1 {
						return Result{Values: []lua.LValue{lua.LString("missing duration")}}
					}
					ms := args[0].(lua.LNumber)
					time.Sleep(time.Duration(ms) * time.Millisecond)
					return Result{Values: []lua.LValue{lua.LString("slept"), ms}}
				})
				return -1
			}),
		)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		// Create wrapped VM with async runner
		wrapped := engine.NewWrappedCVM(vm)
		wrapped.AddLayer(NewAsyncRunner())

		// Import test script with two coroutines
		err = vm.Import(`
           function test_sleep()
               local results = {}

               -- Start first coroutine (longer sleep)
               coroutine.spawn(function()
                   local res1, dur1 = async_sleep(75)
                   results.first = {res1, dur1}
               end)

               -- Start second coroutine (shorter sleep)
               coroutine.spawn(function()
                   local res2, dur2 = async_sleep(25)
                   results.second = {res2, dur2}
               end)

				local res3, dur3 = async_sleep(100)
				results.third = {res3, dur3}

               return results
           end
       `, "test", "test_sleep")

		if err != nil {
			t.Fatal(err)
		}

		start := time.Now()
		result, err := wrapped.Execute(context.Background(), "test_sleep")
		duration := time.Since(start)
		if err != nil {
			t.Fatal(err)
		}

		// Verify results
		resultTable := result.(*lua.LTable)

		// Check first coroutine results
		firstRes := resultTable.RawGetString("first").(*lua.LTable)
		if firstRes.RawGetInt(1).String() != "slept" {
			t.Error("unexpected result from first coroutine")
		}
		if firstRes.RawGetInt(2).(lua.LNumber) != 75 {
			t.Error("unexpected duration from first coroutine")
		}

		// Check second coroutine results
		secondRes := resultTable.RawGetString("second").(*lua.LTable)
		if secondRes.RawGetInt(1).String() != "slept" {
			t.Error("unexpected result from second coroutine")
		}
		if secondRes.RawGetInt(2).(lua.LNumber) != 25 {
			t.Error("unexpected duration from second coroutine")
		}

		// Check second coroutine results
		thirdRes := resultTable.RawGetString("third").(*lua.LTable)
		if thirdRes.RawGetInt(1).String() != "slept" {
			t.Error("unexpected result from third result")
		}
		if thirdRes.RawGetInt(2).(lua.LNumber) != 100 {
			t.Error("unexpected duration from third result")
		}

		// Verify execution time - should be closer to 100ms than 150ms
		// since coroutines run concurrently
		if duration >= 150*time.Millisecond {
			t.Errorf("coroutines appear to be running sequentially, took %v", duration)
		}
	})

}

// ----------
func createVM(t *testing.T) *engine.CoroutineVM {
	log := zap.NewNop()
	vm, err := engine.NewCVM(log,
		engine.WithPreloaded("channel", channel.NewChannelModule().Loader),
		engine.WithGlobalFunction("async_double", func(L *lua.LState) int {
			WrapAsync(L, func(args []lua.LValue) Result {
				value := args[0].(lua.LNumber)
				time.Sleep(100 * time.Millisecond)
				return Result{Values: []lua.LValue{lua.LNumber(value * 2)}}
			})
			return -1
		}),
	)
	assert.NoError(t, err)
	return vm
}

func TestRunner_ChannelLayer(t *testing.T) {
	testScript := `
        function test_layers()
            -- Channel for communication
            local ch = channel.new(1)

            -- Spawn worker that does async operation
            coroutine.spawn(function()
               local doubled = async_double(5)
                ch:send(doubled)
            end)
            
            -- Wait for result
            local result = ch:receive()
            return result
        end
    `

	t.Run("channel first, async second", func(t *testing.T) {
		vm := createVM(t)
		defer vm.Close()

		wrapped := engine.NewWrappedCVM(vm)
		wrapped.AddLayer(NewAsyncRunner())

		wrapped.AddLayer(channel.NewRuntime())

		err := vm.Import(testScript, "test", "test_layers")
		assert.NoError(t, err)

		result, err := wrapped.Execute(context.Background(), "test_layers")
		assert.NoError(t, err)
		assert.NotNil(t, result)

		numValue, ok := result.(lua.LNumber)
		assert.True(t, ok, "expected number result")
		assert.Equal(t, float64(10), float64(numValue))
	})

	t.Run("async first, channel second", func(t *testing.T) {
		vm := createVM(t)
		defer vm.Close()

		wrapped := engine.NewWrappedCVM(vm)
		wrapped.AddLayer(NewAsyncRunner())
		wrapped.AddLayer(channel.NewRuntime())

		err := vm.Import(testScript, "test", "test_layers")
		assert.NoError(t, err)

		result, err := wrapped.Execute(context.Background(), "test_layers")
		assert.NoError(t, err)
		assert.NotNil(t, result)

		numValue, ok := result.(lua.LNumber)
		assert.True(t, ok, "expected number result")
		assert.Equal(t, float64(10), float64(numValue))
	})
}
