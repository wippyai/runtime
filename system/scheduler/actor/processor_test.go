package actor

import (
	"context"
	"testing"
	"time"

	pidapi "github.com/wippyai/runtime/api/pid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStateName(t *testing.T) {
	tests := []struct {
		state ProcessState
		want  string
	}{
		{StateReady, "ready"},
		{StateRunning, "running"},
		{StateBlocked, "blocked"},
		{StateIdle, "idle"},
		{StateComplete, "complete"},
		{ProcessState(5), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			assert.Equal(t, tt.want, StateName(tt.state))
		})
	}
}

func TestStateName_WakeupFlagStripped(t *testing.T) {
	// wakeupFlag occupies bit 4; StateName must mask it before switching.
	runningWithWakeup := StateRunning | wakeupFlag
	assert.Equal(t, "running", StateName(runningWithWakeup))

	readyWithWakeup := StateReady | wakeupFlag
	assert.Equal(t, "ready", StateName(readyWithWakeup))
}

func TestEnableDisableStats(t *testing.T) {
	sched := newTestScheduler(1)

	assert.False(t, sched.collectStats.Load())

	sched.EnableStats()
	assert.True(t, sched.collectStats.Load())

	sched.DisableStats()
	assert.False(t, sched.collectStats.Load())
}

func TestListProcesses_Empty(t *testing.T) {
	sched := newTestScheduler(1)
	sched.Start()
	defer testStopScheduler(sched)

	result := sched.ListProcesses()
	assert.Empty(t, result)
}

func TestListProcesses_ActiveProcess(t *testing.T) {
	sched := newTestScheduler(1)
	sched.Start()
	defer testStopScheduler(sched)

	pid := pidapi.PID{UniqID: "list-test"}
	proc, err := sched.Submit(context.Background(), pid, &IdleProcess{}, "", nil)
	require.NoError(t, err)

	// Wait for process to reach idle so it is stable and visible in ListProcesses.
	deadline := time.Now().Add(2 * time.Second)
	for proc.state.Load() != int32(StateIdle) && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	require.Equal(t, int32(StateIdle), proc.state.Load(), "process should be idle")

	infos := sched.ListProcesses()
	require.NotEmpty(t, infos)

	var found bool
	for _, info := range infos {
		if info.PID == pid {
			found = true
			assert.NotEmpty(t, info.State)
			break
		}
	}
	assert.True(t, found, "submitted process not found in ListProcesses")
}

func TestListProcesses_ReturnsState(t *testing.T) {
	sched := newTestScheduler(1)
	sched.Start()
	defer testStopScheduler(sched)

	pid := pidapi.PID{UniqID: "idle-list-test"}
	proc, err := sched.Submit(context.Background(), pid, &IdleProcess{}, "", nil)
	require.NoError(t, err)

	// Wait for process to reach idle state.
	deadline := time.Now().Add(2 * time.Second)
	for proc.state.Load() != int32(StateIdle) && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	require.Equal(t, int32(StateIdle), proc.state.Load(), "process should be idle")

	infos := sched.ListProcesses()
	require.NotEmpty(t, infos)

	var found bool
	for _, info := range infos {
		if info.PID == pid {
			found = true
			assert.Equal(t, "idle", info.State)
			break
		}
	}
	assert.True(t, found, "idle process not found in ListProcesses")
}
