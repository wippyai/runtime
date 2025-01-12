package channel

import (
	"context"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
	"testing"
)

func TestSelectImmediate(t *testing.T) {
	logger := zap.NewNop()

	vm, err := engine.NewCoroutineVM(
		context.Background(),
		logger,
		engine.WithPreloaded("channel", NewChannelModule().Loader),
	)
	assert.NoError(t, err)
	defer vm.Close()

	err = vm.PushScript(`
		-- Create two buffered channels
		local ch1 = channel.new(1)
		local ch2 = channel.new(1)

		-- Send a value to ch1
		ch1:send("msg1")
		coroutine.yield("value_buffered")

		-- Print debug info before select
		print("ch1:", ch1)
		print("ch2:", ch2)
		
		local case1 = ch1:case_receive()
		local case2 = ch2:case_receive()
		
		print("case1 channel:", case1)
		print("case2 channel:", case2)

		-- Try select on both channels
		local result = channel.select({
			case1,
			case2
		})

		-- Should immediately select ch1 since it has a value
		print("Selected channel:", result.channel)
		print("Original ch1:", ch1)
		print("Channel match:", result.channel == ch1)
		
		assert(result.channel == ch1, "wrong channel selected")
		assert(result.value == "msg1", "wrong value received")
		assert(result.ok == true, "receive should succeed")
		coroutine.yield("select_complete")

	`, "test")
	assert.NoError(t, err)

	runtime := NewRuntime()
	tasks, err := runtime.Step(vm)
	assert.NoError(t, err)

	var yields []string
	for len(tasks) > 0 {
		for _, task := range tasks {
			if len(task.Yielded) > 0 {
				yields = append(yields, task.Yielded[0].String())
			}
		}
		tasks, err = runtime.Step(vm, tasks...)
		assert.NoError(t, err)
	}

	expectedOrder := []string{
		"value_buffered",
		"select_complete",
	}

	assert.Equal(t, expectedOrder, yields, "yields occurred in unexpected order")
}
