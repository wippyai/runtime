package subscribe

import (
	ctxapi "github.com/ponyruntime/pony/api/context"
	"sync"
	"testing"
	"time"

	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

func setupTestVM(t *testing.T) (*engine.CoroutineVM, *engine.Runner) {
	logger := zap.NewNop()

	// Create VM with required modules
	vm, err := engine.NewCVM(
		logger,
		engine.WithPreloaded("pubsub", NewSubscribeModule().Loader),
		engine.WithPreloaded("channel", channel.NewChannelModule().Loader),
	)
	require.NoError(t, err)

	// Create runner
	runner := engine.NewRunner(vm,
		engine.WithLayer(channel.NewChannelLayer()),
		engine.WithLayer(NewSubscribeLayer()),
	)

	return vm, runner
}

func TestPubSub(t *testing.T) {
	t.Run("single subscriber basic flow", func(t *testing.T) {
		vm, runner := setupTestVM(t)
		defer runner.Close()

		script := `
	       function test()
	           local sub = pubsub.subscribe("test-topic")
	           local msg = sub:receive()
	           return msg
	       end
	   `
		err := vm.Import(script, "test", "test")
		require.NoError(t, err)

		uww, ctx := runner.InitUnitOfWork(ctxapi.NewRootContext())
		defer func() { _ = uww.Close() }()

		var wg sync.WaitGroup
		wg.Add(1)
		var result interface{}

		go func() {
			defer wg.Done()

			exitCh, err := runner.Start(ctx, "test")
			if err != nil {
				result = err
				return
			}

			err = Publish(ctx, "test-topic", lua.LString("hello"))
			if err != nil {
				result = err
				return
			}

			res, err := runner.Run(ctx, exitCh)
			if err != nil {
				result = err
				return
			}
			result = res
		}()

		wg.Wait()
		assert.Equal(t, "hello", result.(lua.LValue).String())
	})

	t.Run("prevent duplicate topic subscription", func(t *testing.T) {
		vm, runner := setupTestVM(t)
		defer runner.Close()

		script := `
	      function test()
	          local sub1 = pubsub.subscribe("test-topic")
	          local sub2 = pubsub.subscribe("test-topic") -- should fail
	          return "shouldn't reach here"
	      end
	  `
		err := vm.Import(script, "test", "test")
		require.NoError(t, err)

		uw, ctx := runner.InitUnitOfWork(ctxapi.NewRootContext())
		defer func() { _ = uw.Close() }()

		exitCh, err := runner.Start(ctx, "test")
		require.NoError(t, err)

		_, err = runner.Run(ctx, exitCh)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "already has an active subscription")
	})

	t.Run("unsubscribe flow", func(t *testing.T) {
		vm, runner := setupTestVM(t)
		defer runner.Close()

		script := `
	       function test()
	           local sub = pubsub.subscribe("test-topic")
	           local ok = pubsub.unsubscribe(sub)
	           -- Verify unsubscribe
	           return ok
	       end
	   `
		err := vm.Import(script, "test", "test")
		require.NoError(t, err)

		uw, ctx := runner.InitUnitOfWork(ctxapi.NewRootContext())
		defer func() { _ = uw.Close() }()

		exitCh, err := runner.Start(ctx, "test")
		require.NoError(t, err)

		result, err := runner.Run(ctx, exitCh)
		require.NoError(t, err)
		assert.Equal(t, lua.LTrue, result)
	})

	t.Run("invalid unsubscribe", func(t *testing.T) {
		vm, runner := setupTestVM(t)
		defer runner.Close()

		script := `
	       function test()
	           local ch = channel.new()
	           pubsub.unsubscribe(ch)
	       end
	   `
		err := vm.Import(script, "test", "test")
		require.NoError(t, err)

		uw, ctx := runner.InitUnitOfWork(ctxapi.NewRootContext())
		defer func() { _ = uw.Close() }()

		exitCh, err := runner.Start(ctx, "test")
		require.NoError(t, err)

		_, err = runner.Run(ctx, exitCh)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "channel not found in subscriptions")
	})

	t.Run("multiple messages in order", func(t *testing.T) {
		vm, runner := setupTestVM(t)
		defer runner.Close()

		script := `
	       function test()
	           local sub = pubsub.subscribe("test-topic")
	           local results = {}
	           results[1] = sub:receive()
	           results[2] = sub:receive()
	           results[3] = sub:receive()
	           return table.concat(results, ",")
	       end
	   `
		err := vm.Import(script, "test", "test")
		require.NoError(t, err)

		uw, ctx := runner.InitUnitOfWork(ctxapi.NewRootContext())
		defer func() { _ = uw.Close() }()

		var wg sync.WaitGroup
		wg.Add(1)
		var result interface{}

		go func() {
			defer wg.Done()

			exitCh, err := runner.Start(ctx, "test")
			if err != nil {
				result = err
				return
			}

			// Use context-based Publish
			err = Publish(ctx, "test-topic", lua.LString("one"))
			if err != nil {
				result = err
				return
			}

			err = Publish(ctx, "test-topic", lua.LString("two"))
			if err != nil {
				result = err
				return
			}

			err = Publish(ctx, "test-topic", lua.LString("three"))
			if err != nil {
				result = err
				return
			}

			res, err := runner.Run(ctx, exitCh)
			if err != nil {
				result = err
				return
			}
			result = res
		}()

		wg.Wait()
		assert.Equal(t, "one,two,three", result.(lua.LValue).String())
	})
}

func TestLateSubscription(t *testing.T) {
	vm, runner := setupTestVM(t)
	defer runner.Close()

	script := `
		function test()
			-- Subscribe to topic1 first
			local sub1 = pubsub.subscribe("topic1")

			-- Wait for message on topic1
			local msg = sub1:receive()

			-- Only now subscribe to topic2
			local sub2 = pubsub.subscribe("topic2")

			return msg
		end
	`
	err := vm.Import(script, "test", "test")
	require.NoError(t, err)

	uw, ctx := runner.InitUnitOfWork(ctxapi.NewRootContext())
	defer func() { _ = uw.Close() }()

	var wg sync.WaitGroup
	wg.Add(1)
	var result interface{}

	go func() {
		defer wg.Done()
		exitCh, err := runner.Start(ctx, "test")
		if err != nil {
			result = err
			return
		}

		// First publish to topic2 (no subscriber yet)
		err = Publish(ctx, "topic2", lua.LString("ignored"))
		if err != nil {
			result = err
			return
		}

		time.Sleep(50 * time.Millisecond)

		// Then publish to topic1
		err = Publish(ctx, "topic1", lua.LString("saved"))
		if err != nil {
			result = err
			return
		}

		res, err := runner.Run(ctx, exitCh)
		if err != nil {
			result = err
			return
		}
		result = res
	}()

	wg.Wait()

	if err, ok := result.(error); ok {
		t.Fatal(err)
	}

	assert.Equal(t, "saved", result.(lua.LValue).String())
}

func TestCrossTopicOrdering(t *testing.T) {
	vm, runner := setupTestVM(t)
	defer runner.Close()

	script := `
		function test()
			-- Subscribe to both topics first
			local sub1 = pubsub.subscribe("topic1")
			local sub2 = pubsub.subscribe("topic2")

			-- Queue receives in reverse order of sends
			local msg1 = sub1:receive()
			local msg2 = sub2:receive()

			return msg1 .. "," .. msg2
		end
	`
	err := vm.Import(script, "test", "test")
	require.NoError(t, err)

	uw, ctx := runner.InitUnitOfWork(ctxapi.NewRootContext())
	defer func() { _ = uw.Close() }()

	var wg sync.WaitGroup
	wg.Add(1)
	var result interface{}

	go func() {
		defer wg.Done()
		exitCh, err := runner.Start(ctx, "test")
		if err != nil {
			result = err
			return
		}

		time.Sleep(50 * time.Millisecond) // Let subscriptions set up

		// send in reverse order of receives
		err = Publish(ctx, "topic2", lua.LString("second"))
		if err != nil {
			result = err
			return
		}

		err = Publish(ctx, "topic1", lua.LString("first"))
		if err != nil {
			result = err
			return
		}

		res, err := runner.Run(ctx, exitCh)
		if err != nil {
			result = err
			return
		}
		result = res
	}()

	wg.Wait()

	if err, ok := result.(error); ok {
		t.Fatal(err)
	}

	assert.Equal(t, "first,second", result.(lua.LValue).String())
}

func TestUnsubscribeWithPendingMessages(t *testing.T) {
	vm, runner := setupTestVM(t)
	defer runner.Close()

	script := `
		function test()
			local sub = pubsub.subscribe("test-topic")
			local results = {}

			-- send first message
			results[1] = sub:receive()

			-- Try to receive after external unsubscribe
			local values, ok = sub:receive()
			results[2] = ok and "received" or "closed"

			return table.concat(results, ",")
		end
	`
	err := vm.Import(script, "test", "test")
	require.NoError(t, err)

	uw, ctx := runner.InitUnitOfWork(ctxapi.NewRootContext())
	defer func() { _ = uw.Close() }()

	var wg sync.WaitGroup
	wg.Add(1)
	var result interface{}

	go func() {
		defer wg.Done()

		exitCh, err := runner.Start(ctx, "test")
		if err != nil {
			result = err
			return
		}

		time.Sleep(50 * time.Millisecond) // Let subscriber setup

		// send message before unsubscribe
		err = Publish(ctx, "test-topic", lua.LString("before"))
		if err != nil {
			result = err
			return
		}

		time.Sleep(50 * time.Millisecond)

		// send unsubscribe
		err = Release(ctx, "test-topic")
		if err != nil {
			result = err
			return
		}

		res, err := runner.Run(ctx, exitCh)
		if err != nil {
			result = err
			return
		}
		result = res
	}()

	wg.Wait()
	if err, ok := result.(error); ok {
		t.Fatal(err)
	}
	assert.Equal(t, "before,closed", result.(lua.LValue).String())
}

func TestMultipleTopicsUnsubscribe(t *testing.T) {
	vm, runner := setupTestVM(t)
	defer runner.Close()

	script := `
		function test()
			local sub1 = pubsub.subscribe("topic1")
			local sub2 = pubsub.subscribe("topic2")
			local results = {}

			-- Get first messages
			results[1] = sub1:receive()
			results[2] = sub2:receive()

			-- Get second message from topic2
			results[3] = sub2:receive()

			-- Try receive from unsubscribed topic1
			local values, ok = sub1:receive()
			results[4] = ok and "received" or "closed"

			return table.concat(results, ",")
		end
	`
	err := vm.Import(script, "test", "test")
	require.NoError(t, err)

	uw, ctx := runner.InitUnitOfWork(ctxapi.NewRootContext())
	defer func() { _ = uw.Close() }()

	var wg sync.WaitGroup
	wg.Add(1)
	var result interface{}

	go func() {
		defer wg.Done()

		exitCh, err := runner.Start(ctx, "test")
		if err != nil {
			result = err
			return
		}

		time.Sleep(50 * time.Millisecond) // Let subscribers setup

		// send initial messages
		err = Publish(ctx, "topic1", lua.LString("t1.first"))
		if err != nil {
			result = err
			return
		}

		err = Publish(ctx, "topic2", lua.LString("t2.first"))
		if err != nil {
			result = err
			return
		}

		time.Sleep(50 * time.Millisecond)

		// Release topic1
		err = Release(ctx, "topic1")
		if err != nil {
			result = err
			return
		}

		// send message to topic2 after topic1 unsubscribe
		err = Publish(ctx, "topic2", lua.LString("t2.second"))
		if err != nil {
			result = err
			return
		}

		res, err := runner.Run(ctx, exitCh)
		if err != nil {
			result = err
			return
		}
		result = res
	}()

	wg.Wait()
	if err, ok := result.(error); ok {
		t.Fatal(err)
	}
	assert.Equal(t, "t1.first,t2.first,t2.second,closed", result.(lua.LValue).String())
}

func TestUnsubscribeResubscribe(t *testing.T) {
	vm, runner := setupTestVM(t)
	defer runner.Close()

	script := `
		function test()
			local results = {}

			-- First subscription and message
			local sub1 = pubsub.subscribe("test-topic")
			results[1] = sub1:receive()

			-- First unsubscribe
			pubsub.unsubscribe(sub1)

			-- Try receive after unsubscribe to verify closure
			local values, ok = sub1:receive()
			results[2] = ok and "received" or "closed"

			return table.concat(results, ",")
		end
	`
	err := vm.Import(script, "test", "test")
	require.NoError(t, err)

	uw, ctx := runner.InitUnitOfWork(ctxapi.NewRootContext())
	defer func() { _ = uw.Close() }()

	var wg sync.WaitGroup
	wg.Add(1)
	var result interface{}

	go func() {
		defer wg.Done()

		exitCh, err := runner.Start(ctx, "test")
		if err != nil {
			result = err
			return
		}

		time.Sleep(50 * time.Millisecond)
		err = Publish(ctx, "test-topic", lua.LString("first"))
		if err != nil {
			result = err
			return
		}

		res, err := runner.Run(ctx, exitCh)
		if err != nil {
			result = err
			return
		}
		result = res
	}()

	wg.Wait()
	if err, ok := result.(error); ok {
		t.Fatal(err)
	}
	assert.Equal(t, "first,closed", result.(lua.LValue).String())
}
