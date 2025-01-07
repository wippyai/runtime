package engine

import (
	"context"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
	"log"
	"testing"
)

func TestChannelVM_Basic(t *testing.T) {
	logger := zap.NewNop()

	t.Run("unbuffered channel send/receive", func(t *testing.T) {
		vm, err := NewCoroutineVM(context.Background(), logger)
		if err != nil {
			t.Fatal(err)
		}
		defer vm.Close()

		err = vm.DoString(`
			local ch = channel.new(0)  -- unbuffered channel
			
			-- Sender
			coroutine.spawn(function()
				print("------------------sending hello")
				ch:send("hello")
				print("------------------sent hello")	
				coroutine.yield("send_complete")
				print("------------------send complete")
			end)
			
			-- Receiver
			coroutine.spawn(function()
				print("------------------receiving hello")
				local msg, ok = ch:receive()
				print("------------------received hello")
				assert(ok, "expected successful receive")
				assert(msg == "hello", "wrong message received")
				coroutine.yield("receive_complete")
			end)
		`, "test")

		if err != nil {
			t.Fatal(err)
		}

		// Get initial yielded tasks
		tasks := vm.GetYieldedTasks()
		assert.Equal(t, 2, len(tasks), "expected 2 yielded tasks")

		// Step all tasks until completion
		for len(tasks) > 0 {
			var err error
			tasks, err = vm.Step(tasks...)
			if err != nil {
				log.Printf("error stepping tasks: %v", err)
				t.Fatal(err)
			}
		}
	})

	//t.Run("buffered channel", func(t *testing.T) {
	//	vm, err := NewCoroutineVM(context.Background(), logger)
	//	if err != nil {
	//		t.Fatal(err)
	//	}
	//	defer vm.Close()
	//
	//	err = vm.DoString(`
	//		local ch = channel.new(1)  -- buffered channel with capacity 1
	//
	//		-- Sender can complete immediately
	//		coroutine.spawn(function()
	//			ch:send("buffered")
	//			coroutine.yield("send_complete")
	//		end)
	//
	//		-- Receiver gets buffered value
	//		coroutine.spawn(function()
	//			local msg, ok = ch:receive()
	//			assert(ok, "expected successful receive")
	//			assert(msg == "buffered", "wrong message received")
	//			coroutine.yield("receive_complete")
	//		end)
	//	`, "test")
	//
	//	if err != nil {
	//		t.Fatal(err)
	//	}
	//
	//	tasks := vm.GetYieldedTasks()
	//
	//	// Step all tasks until completion
	//	for len(tasks) > 0 {
	//		var err error
	//		tasks, err = vm.Step(tasks...)
	//		if err != nil {
	//			t.Fatal(err)
	//		}
	//	}
	//})
	//
	//t.Run("closed channel", func(t *testing.T) {
	//	vm, err := NewCoroutineVM(context.Background(), logger)
	//	if err != nil {
	//		t.Fatal(err)
	//	}
	//	defer vm.Close()
	//
	//	err = vm.DoString(`
	//		local ch = channel.new(0)
	//
	//		-- Receiver task
	//		coroutine.spawn(function()
	//			ch:close()
	//			local msg, ok = ch:receive()
	//			assert(not ok, "expected closed channel receive")
	//			assert(msg == nil, "expected nil from closed channel")
	//			coroutine.yield("receive_after_close")
	//		end)
	//	`, "test")
	//
	//	if err != nil {
	//		t.Fatal(err)
	//	}
	//
	//	tasks := vm.GetYieldedTasks()
	//	for len(tasks) > 0 {
	//		var err error
	//		tasks, err = vm.Step(tasks...)
	//		if err != nil {
	//			t.Fatal(err)
	//		}
	//	}
	//})
}
