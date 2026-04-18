// SPDX-License-Identifier: MPL-2.0

package queue

import (
	"context"
	"sync"
	"testing"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/attrs"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	queueapi "github.com/wippyai/runtime/api/queue"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/security"
)

// mockManager implements queueapi.Manager for testing
type mockManager struct {
	driver    queueapi.Driver
	queues    map[string]bool
	published []*publishedMsg
	mu        sync.Mutex
}

// mockInfoDriver implements queueapi.Driver for tests that exercise
// queue.info — only GetQueueInfo returns non-trivial data; the other
// methods are no-op stubs.
type mockInfoDriver struct {
	info attrs.Bag
}

func (d *mockInfoDriver) Publish(context.Context, registry.ID, ...*queueapi.Message) error {
	return nil
}

func (d *mockInfoDriver) Attach(context.Context, registry.ID, *queueapi.ConsumerOptions, chan<- *queueapi.Delivery) (context.CancelFunc, error) {
	return func() {}, nil
}

func (d *mockInfoDriver) DeclareQueue(context.Context, registry.ID, *queueapi.Config) error {
	return nil
}

func (d *mockInfoDriver) GetQueueInfo(context.Context, registry.ID) (attrs.Attributes, error) {
	return d.info, nil
}

type publishedMsg struct {
	message *queueapi.Message
	queueID registry.ID
}

func newMockManager() *mockManager {
	return &mockManager{
		queues: make(map[string]bool),
	}
}

func (m *mockManager) Publish(_ context.Context, queue registry.ID, msgs ...*queueapi.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.queues[queue.String()] {
		return queueapi.ErrQueueNotFound
	}

	for _, msg := range msgs {
		m.published = append(m.published, &publishedMsg{queueID: queue, message: msg})
	}
	return nil
}

func (m *mockManager) GetDriver(registry.ID) (queueapi.Driver, bool) {
	if m.driver != nil {
		return m.driver, true
	}
	return nil, false
}

func (m *mockManager) GetQueue(id registry.ID) (*queueapi.Queue, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.queues[id.String()] {
		return &queueapi.Queue{ID: id}, true
	}
	return nil, false
}

func (m *mockManager) addQueue(_ string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.queues["test:myqueue"] = true
}

func (m *mockManager) getPublished() []*publishedMsg {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.published
}

func (m *mockManager) RegisterInterceptor(_ string, _ queueapi.PublishInterceptor, _ int) {}

func (m *mockManager) UnregisterInterceptor(_ string) {}

func setupState() *lua.LState {
	l := lua.NewState()
	lua.OpenErrors(l)
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)
	return l
}

func setupStateWithManager(mgr *mockManager) *lua.LState {
	l := lua.NewState()
	lua.OpenErrors(l)
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	ctx := ctxapi.NewRootContext()
	appCtx := ctxapi.NewAppContext()
	ctx = ctxapi.WithAppContext(ctx, appCtx)
	ctx = security.SetStrictMode(ctx, false)
	ctx = queueapi.WithManager(ctx, mgr)
	l.SetContext(ctx)

	return l
}

func setupStateWithDelivery(msg *queueapi.Message) *lua.LState {
	return setupStateWithDeliveryCounters(msg, nil, nil)
}

func setupStateWithDeliveryCounters(msg *queueapi.Message, ackCount, nackCount *int) *lua.LState {
	l := lua.NewState()
	lua.OpenErrors(l)
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	ctx := ctxapi.NewRootContext()
	appCtx := ctxapi.NewAppContext()
	ctx = ctxapi.WithAppContext(ctx, appCtx)
	ctx = security.SetStrictMode(ctx, false)

	ctx, _ = ctxapi.OpenFrameContext(ctx)

	delivery := &queueapi.Delivery{
		Message: msg,
		Ack: func(context.Context) error {
			if ackCount != nil {
				*ackCount++
			}
			return nil
		},
		Nack: func(context.Context) error {
			if nackCount != nil {
				*nackCount++
			}
			return nil
		},
	}
	_ = queueapi.WithDelivery(ctx, delivery)

	l.SetContext(ctx)
	return l
}

func TestModuleLoads(t *testing.T) {
	l := setupState()
	defer l.Close()

	mod := l.GetGlobal("queue")
	if mod.Type() != lua.LTTable {
		t.Fatal("queue module not registered")
	}

	modTbl := mod.(*lua.LTable)
	if modTbl.RawGetString("publish").Type() != lua.LTFunction {
		t.Error("publish function not registered")
	}
	if modTbl.RawGetString("message").Type() != lua.LTFunction {
		t.Error("message function not registered")
	}
}

func TestModuleReuse(t *testing.T) {
	l1 := lua.NewState()
	defer l1.Close()
	l2 := lua.NewState()
	defer l2.Close()

	tbl, _ := Module.Build()
	l1.SetGlobal(Module.Name, tbl)
	l2.SetGlobal(Module.Name, tbl)

	mod1 := l1.GetGlobal("queue").(*lua.LTable)
	mod2 := l2.GetGlobal("queue").(*lua.LTable)

	if mod1 != mod2 {
		t.Error("module table should be reused across states")
	}
}

func TestModuleImmutable(t *testing.T) {
	l := setupState()
	defer l.Close()

	mod := l.GetGlobal("queue").(*lua.LTable)
	if !mod.Immutable {
		t.Error("module table should be immutable")
	}
}

func TestPublishNoContext(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	lua.OpenErrors(l)
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local ok, err = queue.publish("test:myqueue", {data = "test"})
		if ok ~= nil then
			error("expected nil result")
		end
		if not err then
			error("expected error")
		end
		if err:kind() ~= errors.INVALID then
			error("expected INVALID error kind, got: " .. tostring(err:kind()))
		end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestPublishNoManager(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	lua.OpenErrors(l)
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	ctx := context.Background()
	appCtx := ctxapi.NewAppContext()
	ctx = ctxapi.WithAppContext(ctx, appCtx)
	l.SetContext(ctx)

	err := l.DoString(`
		local ok, err = queue.publish("test:myqueue", {data = "test"})
		if ok ~= nil then
			error("expected nil result")
		end
		if not err then
			error("expected error")
		end
		if err:kind() ~= errors.INVALID then
			error("expected INVALID error kind, got: " .. tostring(err:kind()))
		end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestPublishEmptyQueueID(t *testing.T) {
	mgr := newMockManager()
	l := setupStateWithManager(mgr)
	defer l.Close()

	err := l.DoString(`
		local ok, err = queue.publish("", {data = "test"})
		if ok ~= nil then
			error("expected nil result")
		end
		if not err then
			error("expected error")
		end
		if err:kind() ~= errors.INVALID then
			error("expected INVALID error kind")
		end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestPublishQueueNotFound(t *testing.T) {
	mgr := newMockManager()
	l := setupStateWithManager(mgr)
	defer l.Close()

	err := l.DoString(`
		local ok, err = queue.publish("test:nonexistent", {data = "test"})
		if ok ~= nil then
			error("expected nil result")
		end
		if not err then
			error("expected error")
		end
		if err:kind() ~= errors.INTERNAL then
			error("expected INTERNAL error kind, got: " .. tostring(err:kind()))
		end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestPublishSuccess(t *testing.T) {
	mgr := newMockManager()
	mgr.addQueue("test:myqueue")
	l := setupStateWithManager(mgr)
	defer l.Close()

	err := l.DoString(`
		local ok, err = queue.publish("test:myqueue", {name = "test", value = 123})
		if err then
			error("unexpected error: " .. tostring(err))
		end
		if ok ~= true then
			error("expected true result")
		end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}

	published := mgr.getPublished()
	if len(published) != 1 {
		t.Errorf("expected 1 published message, got %d", len(published))
	}
	if published[0].queueID.String() != "test:myqueue" {
		t.Errorf("expected queue ID 'test:myqueue', got '%s'", published[0].queueID.String())
	}
}

func TestPublishWithHeaders(t *testing.T) {
	mgr := newMockManager()
	mgr.addQueue("test:myqueue")
	l := setupStateWithManager(mgr)
	defer l.Close()

	err := l.DoString(`
		local ok, err = queue.publish("test:myqueue", "hello", {
			priority = 5,
			correlation_id = "abc123"
		})
		if err then
			error("unexpected error: " .. tostring(err))
		end
		if ok ~= true then
			error("expected true result")
		end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}

	published := mgr.getPublished()
	if len(published) != 1 {
		t.Errorf("expected 1 published message, got %d", len(published))
	}

	msg := published[0].message
	priority, ok := msg.Headers.Get("priority")
	if !ok {
		t.Error("expected priority header to exist")
	} else {
		// Lua integers come through as int64, floats as float64
		switch p := priority.(type) {
		case int64:
			if p != 5 {
				t.Errorf("expected priority header 5, got %v", p)
			}
		case float64:
			if p != 5 {
				t.Errorf("expected priority header 5, got %v", p)
			}
		default:
			t.Errorf("unexpected priority type: %T", priority)
		}
	}

	corrID, ok := msg.Headers.Get("correlation_id")
	if !ok {
		t.Error("expected correlation_id header to exist")
	} else if corrID != "abc123" {
		t.Errorf("expected correlation_id header 'abc123', got %v", corrID)
	}
}

func TestMessageNoDelivery(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	lua.OpenErrors(l)
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	ctx := context.Background()
	appCtx := ctxapi.NewAppContext()
	ctx = ctxapi.WithAppContext(ctx, appCtx)
	ctx, _ = ctxapi.OpenFrameContext(ctx)
	l.SetContext(ctx)

	err := l.DoString(`
		local msg, err = queue.message()
		if msg ~= nil then
			error("expected nil result")
		end
		if not err then
			error("expected error")
		end
		if err:kind() ~= errors.INVALID then
			error("expected INVALID error kind")
		end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestMessageSuccess(t *testing.T) {
	msg := queueapi.NewMessageWithID("msg-123", payload.NewPayload("test data", payload.String))
	msg.Headers.Set("custom", "value")

	l := setupStateWithDelivery(msg)
	defer l.Close()

	err := l.DoString(`
		local msg, err = queue.message()
		if err then
			error("unexpected error: " .. tostring(err))
		end
		if msg == nil then
			error("expected message object")
		end

		-- Test id method
		local id = msg:id()
		if id ~= "msg-123" then
			error("expected id 'msg-123', got: " .. tostring(id))
		end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestMessageHeader(t *testing.T) {
	msg := queueapi.NewMessageWithID("msg-456", payload.NewPayload("test", payload.String))
	msg.Headers.Set("priority", 5)
	msg.Headers.Set("tag", "important")

	l := setupStateWithDelivery(msg)
	defer l.Close()

	err := l.DoString(`
		local msg = queue.message()

		-- Test existing header
		local priority = msg:header("priority")
		if priority ~= 5 then
			error("expected priority 5, got: " .. tostring(priority))
		end

		local tag = msg:header("tag")
		if tag ~= "important" then
			error("expected tag 'important', got: " .. tostring(tag))
		end

		-- Test non-existent header
		local missing = msg:header("nonexistent")
		if missing ~= nil then
			error("expected nil for missing header")
		end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestMessageHeaders(t *testing.T) {
	msg := queueapi.NewMessageWithID("msg-789", payload.NewPayload("test", payload.String))
	msg.Headers.Set("key1", "value1")
	msg.Headers.Set("key2", 42)

	l := setupStateWithDelivery(msg)
	defer l.Close()

	err := l.DoString(`
		local msg = queue.message()
		local headers = msg:headers()

		if type(headers) ~= "table" then
			error("expected table, got: " .. type(headers))
		end

		if headers.key1 ~= "value1" then
			error("expected key1='value1', got: " .. tostring(headers.key1))
		end

		if headers.key2 ~= 42 then
			error("expected key2=42, got: " .. tostring(headers.key2))
		end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestMessageToString(t *testing.T) {
	msg := queueapi.NewMessageWithID("test-id", payload.NewPayload("data", payload.String))

	l := setupStateWithDelivery(msg)
	defer l.Close()

	err := l.DoString(`
		local msg = queue.message()
		local str = tostring(msg)
		if str ~= "queue.Message{id=test-id}" then
			error("expected 'queue.Message{id=test-id}', got: " .. str)
		end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestErrorKinds(t *testing.T) {
	l := setupState()
	defer l.Close()

	ctx := context.Background()
	appCtx := ctxapi.NewAppContext()
	ctx = ctxapi.WithAppContext(ctx, appCtx)
	l.SetContext(ctx)

	err := l.DoString(`
		local ok, err = queue.publish("test:q", "data")
		if not err then error("expected error") end

		-- Test error methods exist
		if type(err.kind) ~= "function" then
			error("error should have kind method")
		end
		if type(err.message) ~= "function" then
			error("error should have message method")
		end
		if type(err.retryable) ~= "function" then
			error("error should have retryable method")
		end

		-- Test error values
		if err:retryable() ~= false then
			error("error should not be retryable")
		end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestMessageMethodsExist(t *testing.T) {
	msg := queueapi.NewMessageWithID("test", payload.NewPayload("data", payload.String))

	l := setupStateWithDelivery(msg)
	defer l.Close()

	err := l.DoString(`
		local msg = queue.message()

		-- Verify all methods exist
		if type(msg.id) ~= "function" then error("msg:id should be a method") end
		if type(msg.header) ~= "function" then error("msg:header should be a method") end
		if type(msg.headers) ~= "function" then error("msg:headers should be a method") end
		if type(msg.ack) ~= "function" then error("msg:ack should be a method") end
		if type(msg.nack) ~= "function" then error("msg:nack should be a method") end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestMessageAck(t *testing.T) {
	msg := queueapi.NewMessageWithID("ack-test", payload.NewPayload("data", payload.String))

	var acks, nacks int
	l := setupStateWithDeliveryCounters(msg, &acks, &nacks)
	defer l.Close()

	err := l.DoString(`
		local msg = queue.message()
		local ok, err = msg:ack()
		if err then error("unexpected error: " .. tostring(err)) end
		if ok ~= true then error("expected true") end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
	if acks != 1 {
		t.Errorf("expected 1 ack call, got %d", acks)
	}
	if nacks != 0 {
		t.Errorf("expected 0 nack calls, got %d", nacks)
	}
}

func TestMessageNack(t *testing.T) {
	msg := queueapi.NewMessageWithID("nack-test", payload.NewPayload("data", payload.String))

	var acks, nacks int
	l := setupStateWithDeliveryCounters(msg, &acks, &nacks)
	defer l.Close()

	err := l.DoString(`
		local msg = queue.message()
		local ok, err = msg:nack()
		if err then error("unexpected error: " .. tostring(err)) end
		if ok ~= true then error("expected true") end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
	if nacks != 1 {
		t.Errorf("expected 1 nack call, got %d", nacks)
	}
	if acks != 0 {
		t.Errorf("expected 0 ack calls, got %d", acks)
	}
}

func TestInfoNoContext(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	lua.OpenErrors(l)
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	err := l.DoString(`
		local info, err = queue.info("test:q")
		if info ~= nil then error("expected nil info") end
		if not err then error("expected error") end
		if err:kind() ~= errors.INVALID then error("expected INVALID") end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

// TestInfoReturnsAllStatsKeys asserts queue.info exposes every key from the
// driver's info bag, not a hardcoded subset. Drivers can attach broker-specific
// stats (e.g. "unacked_count", "x-delivery-rate") and callers must see them.
func TestInfoReturnsAllStatsKeys(t *testing.T) {
	info := attrs.NewBag()
	info.Set(queueapi.StatsMessageCount, 10)
	info.Set(queueapi.StatsConsumerCount, 2)
	info.Set(queueapi.StatsReady, true)
	info.Set("unacked_count", 7)
	info.Set("x-delivery-rate", 3.14)

	mgr := newMockManager()
	mgr.driver = &mockInfoDriver{info: info}
	mgr.addQueue("test:myqueue")

	l := setupStateWithManager(mgr)
	defer l.Close()

	err := l.DoString(`
		local info, err = queue.info("test:myqueue")
		if err then error("unexpected error: " .. tostring(err)) end
		if not info then error("expected info table") end
		if info.message_count ~= 10 then error("message_count mismatch") end
		if info.consumer_count ~= 2 then error("consumer_count mismatch") end
		if info.ready ~= true then error("ready mismatch") end
		if info.unacked_count ~= 7 then error("unacked_count missing or wrong: " .. tostring(info.unacked_count)) end
		if info["x-delivery-rate"] ~= 3.14 then error("x-delivery-rate missing") end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestInfoQueueNotFound(t *testing.T) {
	mgr := newMockManager()
	l := setupStateWithManager(mgr)
	defer l.Close()

	err := l.DoString(`
		local info, err = queue.info("test:missing")
		if info ~= nil then error("expected nil info") end
		if not err then error("expected error") end
		if err:kind() ~= errors.INTERNAL then error("expected INTERNAL") end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestPublishStringData(t *testing.T) {
	mgr := newMockManager()
	mgr.addQueue("test:myqueue")
	l := setupStateWithManager(mgr)
	defer l.Close()

	err := l.DoString(`
		local ok, err = queue.publish("test:myqueue", "simple string message")
		if err then
			error("unexpected error: " .. tostring(err))
		end
		if ok ~= true then
			error("expected true result")
		end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}

	published := mgr.getPublished()
	if len(published) != 1 {
		t.Errorf("expected 1 published message, got %d", len(published))
	}
}

func TestPublishNumberData(t *testing.T) {
	mgr := newMockManager()
	mgr.addQueue("test:myqueue")
	l := setupStateWithManager(mgr)
	defer l.Close()

	err := l.DoString(`
		local ok, err = queue.publish("test:myqueue", 42)
		if err then
			error("unexpected error: " .. tostring(err))
		end
		if ok ~= true then
			error("expected true result")
		end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestPublishTableData(t *testing.T) {
	mgr := newMockManager()
	mgr.addQueue("test:myqueue")
	l := setupStateWithManager(mgr)
	defer l.Close()

	err := l.DoString(`
		local ok, err = queue.publish("test:myqueue", {
			user_id = 123,
			action = "purchase",
			items = {"a", "b", "c"},
			metadata = {
				source = "api",
				timestamp = 1234567890
			}
		})
		if err then
			error("unexpected error: " .. tostring(err))
		end
		if ok ~= true then
			error("expected true result")
		end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestPublishBooleanHeaders(t *testing.T) {
	mgr := newMockManager()
	mgr.addQueue("test:myqueue")
	l := setupStateWithManager(mgr)
	defer l.Close()

	err := l.DoString(`
		local ok, err = queue.publish("test:myqueue", "data", {
			persistent = true,
			urgent = false
		})
		if err then
			error("unexpected error: " .. tostring(err))
		end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}

	published := mgr.getPublished()
	msg := published[0].message

	persistent, ok := msg.Headers.Get("persistent")
	if !ok || persistent != true {
		t.Errorf("expected persistent=true, got %v", persistent)
	}

	urgent, ok := msg.Headers.Get("urgent")
	if !ok || urgent != false {
		t.Errorf("expected urgent=false, got %v", urgent)
	}
}

func TestMessageHeaderTypes(t *testing.T) {
	msg := queueapi.NewMessageWithID("msg-types", payload.NewPayload("test", payload.String))
	msg.Headers.Set("string_val", "hello")
	msg.Headers.Set("int_val", 42)
	msg.Headers.Set("float_val", 3.14)
	msg.Headers.Set("bool_val", true)

	l := setupStateWithDelivery(msg)
	defer l.Close()

	err := l.DoString(`
		local msg = queue.message()

		local str = msg:header("string_val")
		if str ~= "hello" then
			error("expected string 'hello', got: " .. tostring(str))
		end

		local num = msg:header("int_val")
		if num ~= 42 then
			error("expected int 42, got: " .. tostring(num))
		end

		local flt = msg:header("float_val")
		if flt ~= 3.14 then
			error("expected float 3.14, got: " .. tostring(flt))
		end

		local bool = msg:header("bool_val")
		if bool ~= true then
			error("expected bool true, got: " .. tostring(bool))
		end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestMessageEmptyHeaders(t *testing.T) {
	msg := queueapi.NewMessageWithID("msg-empty", payload.NewPayload("test", payload.String))
	msg.Headers = nil

	l := setupStateWithDelivery(msg)
	defer l.Close()

	err := l.DoString(`
		local msg = queue.message()

		local val = msg:header("nonexistent")
		if val ~= nil then
			error("expected nil for header on message with no headers")
		end

		local headers = msg:headers()
		if type(headers) ~= "table" then
			error("expected table for headers, got: " .. type(headers))
		end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestPublishMissingData(t *testing.T) {
	mgr := newMockManager()
	mgr.addQueue("test:myqueue")
	l := setupStateWithManager(mgr)
	defer l.Close()

	err := l.DoString(`
		local ok, err = queue.publish("test:myqueue")
		if ok ~= nil then
			error("expected nil result")
		end
		if not err then
			error("expected error")
		end
		if err:kind() ~= errors.INVALID then
			error("expected INVALID error kind")
		end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestMessageIDReturnsTwoValues(t *testing.T) {
	msg := queueapi.NewMessageWithID("test-id", payload.NewPayload("data", payload.String))

	l := setupStateWithDelivery(msg)
	defer l.Close()

	err := l.DoString(`
		local msg = queue.message()
		local id, err = msg:id()
		if id ~= "test-id" then
			error("expected id 'test-id', got: " .. tostring(id))
		end
		if err ~= nil then
			error("expected nil error, got: " .. tostring(err))
		end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestMessageHeaderReturnsTwoValues(t *testing.T) {
	msg := queueapi.NewMessageWithID("test", payload.NewPayload("data", payload.String))
	msg.Headers.Set("key", "value")

	l := setupStateWithDelivery(msg)
	defer l.Close()

	err := l.DoString(`
		local msg = queue.message()
		local val, err = msg:header("key")
		if val ~= "value" then
			error("expected 'value', got: " .. tostring(val))
		end
		if err ~= nil then
			error("expected nil error")
		end

		local missing, err2 = msg:header("missing")
		if missing ~= nil then
			error("expected nil for missing header")
		end
		if err2 ~= nil then
			error("expected nil error for missing header")
		end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}

func TestMessageHeadersReturnsTwoValues(t *testing.T) {
	msg := queueapi.NewMessageWithID("test", payload.NewPayload("data", payload.String))
	msg.Headers.Set("key", "value")

	l := setupStateWithDelivery(msg)
	defer l.Close()

	err := l.DoString(`
		local msg = queue.message()
		local headers, err = msg:headers()
		if type(headers) ~= "table" then
			error("expected table")
		end
		if err ~= nil then
			error("expected nil error")
		end
	`)
	if err != nil {
		t.Errorf("test failed: %v", err)
	}
}
