package clock

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/pid"
)

type mockTimeRef struct {
	now   time.Time
	start time.Time
}

func (m *mockTimeRef) Now() time.Time       { return m.now }
func (m *mockTimeRef) StartTime() time.Time { return m.start }

func TestWithTimeReference(t *testing.T) {
	t.Run("with frame context", func(t *testing.T) {
		ctx, fc := ctxapi.OpenFrameContext(context.Background())
		defer fc.Close()

		ref := &mockTimeRef{
			now:   time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC),
			start: time.Date(2024, 1, 1, 10, 0, 0, 0, time.UTC),
		}

		err := WithTimeReference(ctx, ref)
		require.NoError(t, err)

		got := GetTimeReference(ctx)
		require.NotNil(t, got)
		assert.Equal(t, ref.Now(), got.Now())
		assert.Equal(t, ref.StartTime(), got.StartTime())
	})

	t.Run("without frame context", func(t *testing.T) {
		ctx := context.Background()
		ref := &mockTimeRef{}

		err := WithTimeReference(ctx, ref)
		assert.NoError(t, err) // Returns nil, not error
	})
}

func TestGetTimeReference(t *testing.T) {
	t.Run("no frame context", func(t *testing.T) {
		ctx := context.Background()
		got := GetTimeReference(ctx)
		assert.Nil(t, got)
	})

	t.Run("frame context without time reference", func(t *testing.T) {
		ctx, fc := ctxapi.OpenFrameContext(context.Background())
		defer fc.Close()

		got := GetTimeReference(ctx)
		assert.Nil(t, got)
	})

	t.Run("frame context with wrong type", func(t *testing.T) {
		ctx, fc := ctxapi.OpenFrameContext(context.Background())
		defer fc.Close()
		_ = fc.Set(timeReferenceKey, "not a time reference")

		got := GetTimeReference(ctx)
		assert.Nil(t, got)
	})
}

func TestCommandIDs(t *testing.T) {
	assert.Equal(t, dispatcher.CommandID(10), Sleep)
	assert.Equal(t, dispatcher.CommandID(14), TickerStart)
	assert.Equal(t, dispatcher.CommandID(16), TickerStop)
	assert.Equal(t, dispatcher.CommandID(18), TimerStart)
	assert.Equal(t, dispatcher.CommandID(19), TimerWait)
	assert.Equal(t, dispatcher.CommandID(20), TimerStop)
	assert.Equal(t, dispatcher.CommandID(21), TimerReset)
}

func TestSleepCmd(t *testing.T) {
	cmd := SleepCmd{Duration: 5 * time.Second}
	assert.Equal(t, Sleep, cmd.CmdID())
}

func TestTickerStartCmd(t *testing.T) {
	cmd := TickerStartCmd{
		Duration: time.Second,
		PID:      pid.PID{Host: "test", UniqID: "123"},
		Topic:    "ticker@123",
	}
	assert.Equal(t, TickerStart, cmd.CmdID())
}

func TestTickerStopCmd(t *testing.T) {
	cmd := TickerStopCmd{TickerID: 42}
	assert.Equal(t, TickerStop, cmd.CmdID())
}

func TestTimerStartCmd(t *testing.T) {
	cmd := TimerStartCmd{
		Duration: time.Second,
		PID:      pid.PID{Host: "test", UniqID: "123"},
		Topic:    "timer@123",
	}
	assert.Equal(t, TimerStart, cmd.CmdID())
}

func TestTimerWaitCmd(t *testing.T) {
	cmd := TimerWaitCmd{TimerID: 42}
	assert.Equal(t, TimerWait, cmd.CmdID())
}

func TestTimerStopCmd(t *testing.T) {
	cmd := TimerStopCmd{TimerID: 42}
	assert.Equal(t, TimerStop, cmd.CmdID())
}

func TestTimerResetCmd(t *testing.T) {
	cmd := TimerResetCmd{TimerID: 42, Duration: 10 * time.Second}
	assert.Equal(t, TimerReset, cmd.CmdID())
}

func TestTickerStartResult(t *testing.T) {
	called := false
	result := TickerStartResult{
		ID:   123,
		Stop: func() { called = true },
	}

	assert.Equal(t, uint64(123), result.ID)
	result.Stop()
	assert.True(t, called)
}

func TestTimerStartResult(t *testing.T) {
	called := false
	result := TimerStartResult{
		ID:   456,
		Stop: func() { called = true },
	}

	assert.Equal(t, uint64(456), result.ID)
	result.Stop()
	assert.True(t, called)
}

func TestErrors(t *testing.T) {
	assert.EqualError(t, ErrTimerNotFound, "timer not found")
	assert.EqualError(t, ErrTickerNotFound, "ticker not found")
}
