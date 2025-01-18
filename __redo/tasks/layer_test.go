package tasks

// TestTaskLayer_BasicOperation tests the basic operation of the task layer
//func TestTaskLayer_BasicOperation(t *testing.T) {
//	logger := zap.NewNop()
//	vm, err := engine.NewCVM(logger,
//		engine.WithPreloaded("channel", channel.NewChannelModule().Loader),
//		engine.WithPreloaded("tasks", NewTaskModule().Loader),
//	)
//	assert.NoError(t, err)
//	defer vm.Close()
//
//	channelRunner := channel.NewChannelRunner()
//	//taskLayer := NewLayer(channelRunner)
//
//	wrapped := engine.NewWrappedCVM(vm,
//		engine.WithLayer(channelRunner),
//		//engine.WithLayer(taskLayer),
//		engine.WithLayer(coroutine.NewCoroutineRunner()),
//		// todo: signal stopper here?
//		// todo: what is the correct oder?
//	)
//
//	err = vm.Import(`
//		function test()
//			print("hei")
//			local ch = tasks.receive()
//			local result = channel.new(1)
//			print("hei2")
//
//			-- Process task
//			coroutine.spawn(function()
//				local task, ok = ch:receive()
//				if not ok then
//print("not ok")
//					result:send("error")
//				end
//print("ok")
//				task:write("processed")
//				task:done()
//				result:send("done")
//			end)
//
//			local out = result:receive()
//			return out
//		end
//	`, "test", "test")
//	assert.NoError(t, err)
//
//	ctx := context.Background()
//	result, err := wrapped.Execute(ctx, "test")
//	log.Printf("result: %v %v", result, err)
//	if assert.NoError(t, err) && assert.NotNil(t, result) {
//		assert.Equal(t, "done", result.String())
//	}
//}
