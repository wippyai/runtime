package async

import (
	"context"
	"github.com/ponyruntime/pony/runtime/lua/engine"
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
				return Result{Values: []lua.LValue{lua.LString("done")}}
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
			coroutine.yield("after_sleep", result)
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

	//t.Run("many coroutines with varying sleep times", func(t *testing.T) {
	//	log := zap.NewNop()
	//
	//	vm, err := engine.NewCVM(log,
	//		engine.WithGlobalFunction("async_sleep", func(L *lua.LState) int {
	//			_ = L.CheckNumber(1)
	//			WrapAsync(L, func(args []lua.LValue) Result {
	//				ms := args[0].(lua.LNumber)
	//				time.Sleep(time.Duration(ms) * time.Millisecond)
	//				return Result{Values: []lua.LValue{lua.LNumber(ms)}}
	//			})
	//			return -1
	//		}),
	//	)
	//	if err != nil {
	//		t.Fatal(err)
	//	}
	//	defer vm.Close()
	//
	//	wrapped := engine.NewWrappedCVM(vm)
	//	wrapped.AddLayer(NewAsyncRunner())
	//
	//	// Create test with multiple coroutines
	//	err = vm.Import(`
	//        function test_multi_sleep()
	//            local results = {}
	//
	//            -- Spawn multiple coroutines with different sleep times
	//            for i = 1, 5 do
	//                coroutine.spawn(function()
	//                    local dur = i * 20  -- 20, 40, 60, 80, 100ms
	//                    local res = async_sleep(dur)
	//                    results[i] = res
	//                end)
	//            end
	//
	//            coroutine.yield("waiting")
	//            return results
	//        end
	//    `, "test", "test_multi_sleep")
	//
	//	if err != nil {
	//		t.Fatal(err)
	//	}
	//
	//	start := time.Now()
	//	result, err := wrapped.Execute(context.Background(), "test_multi_sleep")
	//	duration := time.Since(start)
	//
	//	if err != nil {
	//		t.Fatal(err)
	//	}
	//
	//	// Verify results
	//	resultTable := result.(*lua.LTable)
	//	for i := 1; i <= 5; i++ {
	//		sleepTime := resultTable.RawGetInt(i).(lua.LNumber)
	//		expectedTime := lua.LNumber(i * 20)
	//		if sleepTime != expectedTime {
	//			t.Errorf("coroutine %d: expected sleep time %v, got %v", i, expectedTime, sleepTime)
	//		}
	//	}
	//
	//	// Verify total execution time - should be close to 100ms (longest sleep)
	//	// rather than 300ms (sum of all sleeps)
	//	if duration >= 200*time.Millisecond {
	//		t.Errorf("coroutines appear to be running sequentially, took %v", duration)
	//	}
	//})
}
