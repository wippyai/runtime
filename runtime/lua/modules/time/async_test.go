package time

import (
	"context"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/coroutine"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
	"testing"
	"time"
)

func TestSleepInCoroutines(t *testing.T) {
	t.Run("sleep in coroutines", func(t *testing.T) {
		log := zap.NewNop()

		// Create base VM with sleep function
		vm, err := engine.NewCVM(
			log,
			engine.WithLoader("time", NewTimeModule().Loader),
		)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		// Create wrapped VM with async runner
		wrapped := engine.NewWrappedCVM(vm, engine.WithLayer(coroutine.NewCoroutineRunner()))

		// Import test script with two coroutines
		err = vm.Import(`
		   local time = require("time")

           function test_sleep()
               local results = {}

               -- Start first coroutine (longer sleep)
               coroutine.spawn(function()
                   time.sleep(time.parse_duration("75ms"))	
                   results.first = {"ok1"}
               end)

               -- Start second coroutine (shorter sleep)
               coroutine.spawn(function()
                   time.sleep(time.parse_duration("25ms"))
                   results.second = {"ok2"}
               end)

			   time.sleep(time.parse_duration("100ms"))
               results.third = {"ok3"}

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
		if firstRes.RawGetInt(1).String() != "ok1" {
			t.Error("unexpected result from first coroutine")
		}

		// Check second coroutine results
		secondRes := resultTable.RawGetString("second").(*lua.LTable)
		if secondRes.RawGetInt(1).String() != "ok2" {
			t.Error("unexpected result from second coroutine")
		}

		// Check second coroutine results
		thirdRes := resultTable.RawGetString("third").(*lua.LTable)
		if thirdRes.RawGetInt(1).String() != "ok3" {
			t.Error("unexpected result from third result")
		}

		// Verify execution time - should be closer to 100ms than 150ms
		// since coroutines run concurrently
		if duration >= 150*time.Millisecond {
			t.Errorf("coroutines appear to be running sequentially, took %v", duration)
		}
	})

}
