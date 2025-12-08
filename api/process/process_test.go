package process

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/internal/uniqid"
)

func TestStepOutput_Result(t *testing.T) {
	tests := []struct {
		name   string
		result Payload
		want   any
	}{
		{
			name:   "nil result",
			result: nil,
			want:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var so StepOutput
			if tt.result != nil {
				so.Done(tt.result)
			} else {
				so.Done(nil)
			}

			if tt.result == nil {
				if so.Result() != nil {
					t.Errorf("expected nil result, got %v", so.Result())
				}
				return
			}
		})
	}
}

func TestStepOutput_Reset(t *testing.T) {
	var so StepOutput
	so.Done(nil)
	so.Yield(&mockCommand{id: 1}, 0)

	so.Reset()

	if so.Status() != StepContinue {
		t.Errorf("Status after Reset = %v, want StepContinue", so.Status())
	}
	if so.Result() != nil {
		t.Errorf("Result after Reset = %v, want nil", so.Result())
	}
	if so.Count() != 0 {
		t.Errorf("Count after Reset = %d, want 0", so.Count())
	}
}

func TestStepOutput_Yield(t *testing.T) {
	var so StepOutput

	for i := 0; i < MaxYields+2; i++ {
		so.Yield(&mockCommand{id: CommandID(i)}, uint64(i+1)) //nolint:gosec // test iteration
	}

	if so.Count() != MaxYields+2 {
		t.Errorf("Count = %d, want %d", so.Count(), MaxYields+2)
	}

	yields := so.Yields()
	if len(yields) != MaxYields+2 {
		t.Errorf("len(Yields) = %d, want %d", len(yields), MaxYields+2)
	}
}

func TestStepOutput_Idle(t *testing.T) {
	var so StepOutput
	so.Idle()
	assert.Equal(t, StepIdle, so.Status())
	assert.True(t, so.IsIdle())
	assert.False(t, so.IsDone())
}

func TestStepOutput_Continue(t *testing.T) {
	var so StepOutput
	so.Idle()
	so.Continue()
	assert.Equal(t, StepContinue, so.Status())
	assert.False(t, so.IsIdle())
	assert.False(t, so.IsDone())
}

func TestStepOutput_Done(t *testing.T) {
	var so StepOutput
	result := payload.New("test result")
	so.Done(result)
	assert.Equal(t, StepDone, so.Status())
	assert.True(t, so.IsDone())
	assert.False(t, so.IsIdle())
	assert.Equal(t, result, so.Result())
}

func TestStepOutput_YieldsEmpty(t *testing.T) {
	var so StepOutput
	assert.Nil(t, so.Yields())
	assert.Equal(t, 0, so.Count())
}

func TestStepOutput_YieldsInline(t *testing.T) {
	var so StepOutput
	so.Yield(&mockCommand{id: 1}, 100)
	so.Yield(&mockCommand{id: 2}, 200)

	assert.Equal(t, 2, so.Count())
	yields := so.Yields()
	assert.Len(t, yields, 2)
	assert.Equal(t, uint64(100), yields[0].Tag)
	assert.Equal(t, uint64(200), yields[1].Tag)
}

func TestStepOutput_ForEachYield(t *testing.T) {
	var so StepOutput
	so.Yield(&mockCommand{id: 1}, 100)
	so.Yield(&mockCommand{id: 2}, 200)
	so.Yield(&mockCommand{id: 3}, 300)
	so.Yield(&mockCommand{id: 4}, 400)

	var tags []uint64
	so.ForEachYield(func(y Yield) {
		tags = append(tags, y.Tag)
	})

	assert.Equal(t, []uint64{100, 200, 300, 400}, tags)
}

func TestError_Methods(t *testing.T) {
	t.Run("ErrMaxProcessesExceeded", func(t *testing.T) {
		assert.Equal(t, "max processes limit exceeded", ErrMaxProcessesExceeded.Error())
		assert.Equal(t, KindLimitExceeded, ErrMaxProcessesExceeded.Kind())
		assert.Nil(t, ErrMaxProcessesExceeded.Details())
		assert.Nil(t, ErrMaxProcessesExceeded.Unwrap())
	})

	t.Run("ErrProcessNotFound", func(t *testing.T) {
		assert.Equal(t, "process not found", ErrProcessNotFound.Error())
		assert.Equal(t, KindNotFound, ErrProcessNotFound.Kind())
	})

	t.Run("ErrProcessNotIdle", func(t *testing.T) {
		assert.Equal(t, "process is not idle", ErrProcessNotIdle.Error())
		assert.Equal(t, KindInvalidState, ErrProcessNotIdle.Kind())
	})

	t.Run("ErrProcessClosed", func(t *testing.T) {
		assert.Equal(t, "process closed", ErrProcessClosed.Error())
	})
}

func TestError_WithCause(t *testing.T) {
	cause := errors.New("underlying cause")
	err := ErrProcessNotFound.WithCause(cause)

	assert.Equal(t, "process not found", err.Error())
	assert.Equal(t, KindNotFound, err.Kind())
	assert.Equal(t, cause, err.Unwrap())
	assert.True(t, errors.Is(err, cause))
}

func TestError_WithDetails(t *testing.T) {
	details := ctxapi.NewPairBag([]ctxapi.Pair{{Key: "pid", Value: "12345"}})
	err := ErrProcessNotFound.WithDetails(details)

	assert.Equal(t, "process not found", err.Error())
	assert.NotNil(t, err.Details())
	val, ok := err.Details().Get("pid")
	assert.True(t, ok)
	assert.Equal(t, "12345", val)
}

func TestUnknownCommandError(t *testing.T) {
	err := &UnknownCommandError{CmdID: 999}

	assert.Equal(t, "unknown command: 999", err.Error())
	assert.Equal(t, "NotFound", string(err.Kind()))
	assert.Equal(t, "False", err.Retryable().String())

	details := err.Details()
	assert.NotNil(t, details)
	cmdID, ok := details.Get("command_id")
	assert.True(t, ok)
	assert.Equal(t, 999, cmdID)

	details2 := err.Details()
	assert.Equal(t, details, details2)
}

func TestEventQueue_Basic(t *testing.T) {
	q := NewEventQueue()
	assert.NotNil(t, q)
	assert.Equal(t, uint64(1), q.Generation())
	assert.False(t, q.HasEvents())
}

func TestEventQueue_PushDrain(t *testing.T) {
	q := NewEventQueue()
	gen := q.Generation()

	ok := q.Push(Event{Type: EventYieldComplete, Tag: 1, Data: "result1"}, gen)
	assert.True(t, ok)
	assert.True(t, q.HasEvents())

	ok = q.Push(Event{Type: EventMessage, Data: "msg1"}, gen)
	assert.True(t, ok)

	events := q.Drain()
	assert.Len(t, events, 2)
	assert.Equal(t, uint64(1), events[0].Tag)
	assert.Equal(t, "msg1", events[1].Data)
	assert.False(t, q.HasEvents())

	events2 := q.Drain()
	assert.Nil(t, events2)
}

func TestEventQueue_PushDirect(t *testing.T) {
	q := NewEventQueue()
	q.PushDirect(Event{Type: EventMessage, Data: "direct"})
	assert.True(t, q.HasEvents())

	events := q.Drain()
	assert.Len(t, events, 1)
	assert.Equal(t, "direct", events[0].Data)
}

func TestEventQueue_GenerationCheck(t *testing.T) {
	q := NewEventQueue()
	gen := q.Generation()

	ok := q.Push(Event{Data: "valid"}, gen)
	assert.True(t, ok)

	ok = q.Push(Event{Data: "wrong gen"}, gen+1)
	assert.False(t, ok)

	events := q.Drain()
	assert.Len(t, events, 1)
}

func TestEventQueue_Close(t *testing.T) {
	q := NewEventQueue()
	gen := q.Generation()

	q.Push(Event{Data: "before close"}, gen)
	q.Close()

	ok := q.Push(Event{Data: "after close"}, gen)
	assert.False(t, ok)
}

func TestEventQueue_Reset(t *testing.T) {
	q := NewEventQueue()
	gen1 := q.Generation()

	q.Push(Event{Data: "old"}, gen1)
	q.Reset()

	gen2 := q.Generation()
	assert.NotEqual(t, gen1, gen2)
	assert.False(t, q.HasEvents())

	ok := q.Push(Event{Data: "stale"}, gen1)
	assert.False(t, ok)

	ok = q.Push(Event{Data: "new"}, gen2)
	assert.True(t, ok)
}

func TestEventQueue_Signal(t *testing.T) {
	q := NewEventQueue()
	sig := q.Signal()
	assert.NotNil(t, sig)

	select {
	case <-sig:
		t.Fatal("signal should be empty initially")
	default:
	}

	q.PushDirect(Event{Data: "trigger"})

	select {
	case <-sig:
	default:
		t.Fatal("signal should be triggered after push")
	}
}

func TestEventQueue_Concurrent(t *testing.T) {
	q := NewEventQueue()
	gen := q.Generation()

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			q.Push(Event{Tag: uint64(n)}, gen) //nolint:gosec // test iteration
		}(i)
	}
	wg.Wait()

	events := q.Drain()
	assert.Len(t, events, 100)
}

func TestMessageSender(t *testing.T) {
	q := NewEventQueue()
	sender := q.NewMessageSender()

	ok := sender.Send("hello")
	assert.True(t, ok)

	events := q.Drain()
	assert.Len(t, events, 1)
	assert.Equal(t, EventMessage, events[0].Type)
	assert.Equal(t, "hello", events[0].Data)
}

func TestMessageSender_StaleAfterReset(t *testing.T) {
	q := NewEventQueue()
	sender := q.NewMessageSender()

	ok := sender.Send("before reset")
	assert.True(t, ok)

	q.Reset()

	ok = sender.Send("after reset")
	assert.False(t, ok)
}

func TestContextFunctions(t *testing.T) {
	ctx := context.Background()

	t.Run("nil app context returns nil", func(t *testing.T) {
		assert.Nil(t, GetManager(ctx))
		assert.Nil(t, GetFactory(ctx))
		assert.Nil(t, GetPIDGenerator(ctx))
		assert.Nil(t, GetLifecycleRegistry(ctx))
	})

	t.Run("with app context", func(t *testing.T) {
		appCtx := ctxapi.New()
		ctx := ctxapi.WithApp(context.Background(), appCtx)

		assert.Nil(t, GetManager(ctx))
		assert.Nil(t, GetFactory(ctx))
		assert.Nil(t, GetPIDGenerator(ctx))
		assert.Nil(t, GetLifecycleRegistry(ctx))
	})
}

func TestWithManager(t *testing.T) {
	t.Run("nil app context", func(t *testing.T) {
		ctx := context.Background()
		result := WithManager(ctx, nil)
		assert.Equal(t, ctx, result)
	})

	t.Run("with app context", func(t *testing.T) {
		appCtx := ctxapi.New()
		ctx := ctxapi.WithApp(context.Background(), appCtx)

		mockMgr := &mockManager{}
		result := WithManager(ctx, mockMgr)
		assert.Equal(t, ctx, result)

		mgr := GetManager(ctx)
		assert.Equal(t, mockMgr, mgr)

		WithManager(ctx, &mockManager{})
		mgr2 := GetManager(ctx)
		assert.Equal(t, mockMgr, mgr2)
	})
}

func TestWithFactory(t *testing.T) {
	t.Run("nil app context", func(t *testing.T) {
		ctx := context.Background()
		WithFactory(ctx, nil)
	})

	t.Run("with app context", func(t *testing.T) {
		appCtx := ctxapi.New()
		ctx := ctxapi.WithApp(context.Background(), appCtx)

		mockFac := &mockFactory{}
		WithFactory(ctx, mockFac)

		fac := GetFactory(ctx)
		assert.Equal(t, mockFac, fac)
	})
}

func TestWithPIDGenerator(t *testing.T) {
	t.Run("nil app context", func(t *testing.T) {
		ctx := context.Background()
		result := WithPIDGenerator(ctx, nil)
		assert.Equal(t, ctx, result)
	})

	t.Run("with app context", func(t *testing.T) {
		appCtx := ctxapi.New()
		ctx := ctxapi.WithApp(context.Background(), appCtx)

		gen := uniqid.NewPIDGenerator(1)
		result := WithPIDGenerator(ctx, gen)
		assert.Equal(t, ctx, result)

		retrieved := GetPIDGenerator(ctx)
		assert.Equal(t, gen, retrieved)

		WithPIDGenerator(ctx, uniqid.NewPIDGenerator(2))
		retrieved2 := GetPIDGenerator(ctx)
		assert.Equal(t, gen, retrieved2)
	})
}

func TestWithLifecycleRegistry(t *testing.T) {
	t.Run("nil app context", func(t *testing.T) {
		ctx := context.Background()
		result := WithLifecycleRegistry(ctx, nil)
		assert.Equal(t, ctx, result)
	})

	t.Run("with app context", func(t *testing.T) {
		appCtx := ctxapi.New()
		ctx := ctxapi.WithApp(context.Background(), appCtx)

		mockReg := &mockLifecycleRegistry{}
		result := WithLifecycleRegistry(ctx, mockReg)
		assert.Equal(t, ctx, result)

		reg := GetLifecycleRegistry(ctx)
		assert.Equal(t, mockReg, reg)

		WithLifecycleRegistry(ctx, &mockLifecycleRegistry{})
		reg2 := GetLifecycleRegistry(ctx)
		assert.Equal(t, mockReg, reg2)
	})
}

func TestEventTypes(t *testing.T) {
	assert.Equal(t, EventType(0), EventYieldComplete)
	assert.Equal(t, EventType(1), EventMessage)
}

func TestStepStatus(t *testing.T) {
	assert.Equal(t, StepStatus(0), StepContinue)
	assert.Equal(t, StepStatus(1), StepIdle)
	assert.Equal(t, StepStatus(2), StepDone)
}

func TestSchedulerKind(t *testing.T) {
	assert.Equal(t, SchedulerKind("global"), KindGlobal)
	assert.Equal(t, SchedulerKind("stealing"), KindStealing)
}

func TestStart(t *testing.T) {
	start := Start{
		Source:  registry.NewID("test", "func"),
		Input:   payload.Payloads{payload.New("arg1")},
		Context: []ctxapi.Pair{{Key: "user", Value: "test"}},
	}

	assert.Equal(t, "test", start.Source.NS)
	assert.Equal(t, "func", start.Source.Name)
	assert.Len(t, start.Input, 1)
	assert.Len(t, start.Context, 1)
}

func TestMeta(t *testing.T) {
	meta := Meta{Method: "handler"}
	assert.Equal(t, "handler", meta.Method)
}

func TestFactoryEntry(t *testing.T) {
	entry := FactoryEntry{
		Factory: func() (Process, error) { return nil, nil },
		Meta:    Meta{Method: "test"},
	}
	assert.NotNil(t, entry.Factory)
	assert.Equal(t, "test", entry.Meta.Method)
}

type mockCommand struct {
	id CommandID
}

func (c *mockCommand) CmdID() CommandID { return c.id }

type mockManager struct{}

func (m *mockManager) Start(_ context.Context, _ *Start) (relay.PID, error) {
	return relay.PID{}, nil
}
func (m *mockManager) Cancel(_ context.Context, _, _ relay.PID, _ time.Time) error { return nil }
func (m *mockManager) Terminate(_ context.Context, _ relay.PID) error              { return nil }

type mockFactory struct{}

func (f *mockFactory) Create(_ registry.ID) (Process, *Meta, error) {
	return nil, nil, nil
}

type mockLifecycleRegistry struct{}

func (r *mockLifecycleRegistry) OnStart(_ context.Context, _ relay.PID, _ Process)            {}
func (r *mockLifecycleRegistry) OnComplete(_ context.Context, _ relay.PID, _ *runtime.Result) {}
func (r *mockLifecycleRegistry) Register(_ string, _ Lifecycle)                               {}
func (r *mockLifecycleRegistry) Unregister(_ string)                                          {}
