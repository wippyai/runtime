package queue

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	queueapi "github.com/wippyai/runtime/api/queue"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/runtime/lua/engine"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

func TestQueueModule(t *testing.T) {
	logger := zap.NewNop()

	t.Run("module creation and loading", func(t *testing.T) {
		mod := NewModule()
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		err = vm.DoString(context.Background(), `
			local queue = require("queue")
			assert(type(queue) == "table")
			assert(type(queue.publish) == "function")
			assert(type(queue.message) == "function")
		`, "test")
		assert.NoError(t, err)
	})

	t.Run("publish success", func(t *testing.T) {
		mod := NewModule()
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		// Create mock queue manager
		mockMgr := &mockQueueManager{}

		// Create context with queue manager (need RootContext for AppContext)
		ctx := ctxapi.NewRootContext()
		ctx = queueapi.WithManager(ctx, mockMgr)

		script := `
			local queue = require("queue")
			function test()
				local ok, err = queue.publish("app:tasks", {order_id = "123", amount = 100})
				return {success = ok, error = err}
			end
			return test
		`
		err = vm.Import(script, "test", "test")
		require.NoError(t, err)

		result, err := vm.Execute(ctx, "test")
		require.NoError(t, err)
		require.IsType(t, &lua.LTable{}, result)

		tbl := result.(*lua.LTable)
		success := tbl.RawGetString("success")
		assert.Equal(t, lua.LTrue, success)

		// Verify publish was called
		assert.True(t, mockMgr.publishCalled)
		assert.Equal(t, "app:tasks", mockMgr.lastQueueID.String())
	})

	t.Run("publish without queue manager", func(t *testing.T) {
		mod := NewModule()
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		// No queue manager in context
		ctx := context.Background()

		script := `
			local queue = require("queue")
			function test()
				local ok, err = queue.publish("app:tasks", {data = "test"})
				return {success = ok, error = err}
			end
			return test
		`
		err = vm.Import(script, "test", "test")
		require.NoError(t, err)

		result, err := vm.Execute(ctx, "test")
		require.NoError(t, err)
		require.IsType(t, &lua.LTable{}, result)

		tbl := result.(*lua.LTable)
		success := tbl.RawGetString("success")
		errMsg := tbl.RawGetString("error")

		assert.Equal(t, lua.LNil, success)
		assert.Contains(t, errMsg.String(), "queue manager not found")
	})

	t.Run("message without delivery", func(t *testing.T) {
		mod := NewModule()
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		// No delivery in context
		ctx := context.Background()

		script := `
			local queue = require("queue")
			function test()
				local msg, err = queue.message()
				return {message = msg, error = err}
			end
			return test
		`
		err = vm.Import(script, "test", "test")
		require.NoError(t, err)

		result, err := vm.Execute(ctx, "test")
		require.NoError(t, err)
		require.IsType(t, &lua.LTable{}, result)

		tbl := result.(*lua.LTable)
		msg := tbl.RawGetString("message")
		errMsg := tbl.RawGetString("error")

		assert.Equal(t, lua.LNil, msg)
		assert.Contains(t, errMsg.String(), "no delivery found")
	})

	t.Run("message with delivery", func(t *testing.T) {
		mod := NewModule()
		vm, err := engine.NewVM(logger, engine.WithLoader(mod.Name(), mod.Loader))
		require.NoError(t, err)
		defer vm.Close()

		// Create delivery with message
		msg := queueapi.NewMessage(payload.New("test"))
		msg.ID = "msg-123"
		msg.Headers = attrs.NewBag()
		msg.Headers.Set(queueapi.HeaderTimestamp, int64(1234567890))
		msg.Headers.Set(queueapi.HeaderDeliveryCount, 1)
		msg.Headers.Set(queueapi.HeaderCorrelationID, "corr-456")

		delivery := &queueapi.Delivery{
			Message: msg,
			Ack:     func(ctx context.Context) error { return nil },
			Nack:    func(ctx context.Context) error { return nil },
		}

		// Create context with delivery in frame (need RootContext)
		ctx := ctxapi.NewRootContext()
		ctx, fc := ctxapi.OpenFrameContext(ctx)
		pair := queueapi.DeliveryPair(delivery)
		err = fc.Set(pair.Key, pair.Value)
		require.NoError(t, err)

		script := `
			local queue = require("queue")
			function test()
				local msg, err = queue.message()
				if err then
					return {error = err}
				end

				local id = msg:id()
				local timestamp = msg:header("timestamp")
				local delivery_count = msg:header("delivery_count")
				local correlation_id = msg:header("correlation_id")
				local headers = msg:headers()

				return {
					id = id,
					timestamp = timestamp,
					delivery_count = delivery_count,
					correlation_id = correlation_id,
					has_headers = headers ~= nil
				}
			end
			return test
		`
		err = vm.Import(script, "test", "test")
		require.NoError(t, err)

		result, err := vm.Execute(ctx, "test")
		require.NoError(t, err)
		require.IsType(t, &lua.LTable{}, result)

		tbl := result.(*lua.LTable)
		id := tbl.RawGetString("id").String()
		timestamp := int64(tbl.RawGetString("timestamp").(lua.LNumber))
		deliveryCount := int(tbl.RawGetString("delivery_count").(lua.LNumber))
		correlationID := tbl.RawGetString("correlation_id").String()
		hasHeaders := bool(tbl.RawGetString("has_headers").(lua.LBool))

		assert.Equal(t, "msg-123", id)
		assert.Equal(t, int64(1234567890), timestamp)
		assert.Equal(t, 1, deliveryCount)
		assert.Equal(t, "corr-456", correlationID)
		assert.True(t, hasHeaders)
	})
}

// mockQueueManager is a mock implementation of queue.Manager for testing
type mockQueueManager struct {
	publishCalled bool
	lastQueueID   registry.ID
	lastMessage   *queueapi.Message
}

func (m *mockQueueManager) Publish(ctx context.Context, queue registry.ID, msgs ...*queueapi.Message) error {
	m.publishCalled = true
	m.lastQueueID = queue
	if len(msgs) > 0 {
		m.lastMessage = msgs[0]
	}
	return nil
}

func (m *mockQueueManager) GetDriver(id registry.ID) (queueapi.Driver, bool) {
	return nil, false
}

func (m *mockQueueManager) GetQueue(id registry.ID) (*queueapi.Queue, bool) {
	return nil, false
}
