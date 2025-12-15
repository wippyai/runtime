package sandbox

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/runtime/lua/evalhost"
)

func TestModule_Info(t *testing.T) {
	info := Module.Info()
	assert.Equal(t, "eval_sandbox", info.Name)
	assert.Contains(t, info.Class, "process")
	assert.Contains(t, info.Class, "deterministic")
}

func TestMockClock_Creation(t *testing.T) {
	// With zero start time, should use current time
	c1 := NewMockClock(0)
	require.NotNil(t, c1)
	assert.True(t, c1.Now().Unix() > 0)

	// With specific start time
	startTime := time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	c2 := NewMockClock(startTime.UnixNano())
	require.NotNil(t, c2)
	assert.Equal(t, startTime.UnixNano(), c2.NowNano())
}

func TestMockClock_TimeReference(t *testing.T) {
	startTime := time.Date(2024, 6, 15, 10, 30, 0, 0, time.UTC)
	c := NewMockClock(startTime.UnixNano())

	// Now() and StartTime() should match initially (compare UnixNano to avoid timezone issues)
	assert.Equal(t, startTime.UnixNano(), c.Now().UnixNano())
	assert.Equal(t, startTime.UnixNano(), c.StartTime().UnixNano())
}

func TestMockClock_Advance(t *testing.T) {
	startTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	c := NewMockClock(startTime.UnixNano())

	// Advance by 1 hour
	c.Advance(time.Hour)
	expected := startTime.Add(time.Hour)
	assert.Equal(t, expected.UnixNano(), c.Now().UnixNano())

	// StartTime should remain unchanged
	assert.Equal(t, startTime.UnixNano(), c.StartTime().UnixNano())
}

func TestMockClock_AdvanceNano(t *testing.T) {
	startTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	c := NewMockClock(startTime.UnixNano())

	// Advance by 500ms in nanoseconds
	c.AdvanceNano(500 * int64(time.Millisecond))
	expected := startTime.Add(500 * time.Millisecond)
	assert.Equal(t, expected.UnixNano(), c.Now().UnixNano())
}

func TestMockClock_Set(t *testing.T) {
	startTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	c := NewMockClock(startTime.UnixNano())

	newTime := time.Date(2025, 6, 15, 12, 30, 0, 0, time.UTC)
	c.Set(newTime)
	assert.Equal(t, newTime.UnixNano(), c.Now().UnixNano())

	// StartTime should remain unchanged
	assert.Equal(t, startTime.UnixNano(), c.StartTime().UnixNano())
}

func TestMockClock_SetNano(t *testing.T) {
	startTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	c := NewMockClock(startTime.UnixNano())

	newTime := time.Date(2025, 6, 15, 12, 30, 0, 0, time.UTC)
	c.SetNano(newTime.UnixNano())
	assert.Equal(t, newTime.UnixNano(), c.NowNano())
}

func TestMockClock_Elapsed(t *testing.T) {
	startTime := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	c := NewMockClock(startTime.UnixNano())

	// Initial elapsed should be 0
	assert.Equal(t, time.Duration(0), c.Elapsed())

	// Advance and check elapsed
	c.Advance(5 * time.Second)
	assert.Equal(t, 5*time.Second, c.Elapsed())

	c.Advance(10 * time.Minute)
	assert.Equal(t, 5*time.Second+10*time.Minute, c.Elapsed())
}

func TestCompileYield_Pool(t *testing.T) {
	y := AcquireCompileYield()
	require.NotNil(t, y)

	y.Source = "test source"
	y.Method = "handle"
	y.Modules = []string{"json", "time"}

	ReleaseCompileYield(y)

	y2 := AcquireCompileYield()
	assert.Empty(t, y2.Source)
	assert.Empty(t, y2.Method)
	assert.Nil(t, y2.Modules)
	ReleaseCompileYield(y2)
}

func TestCompileYield_ToCommand(t *testing.T) {
	y := AcquireCompileYield()
	defer ReleaseCompileYield(y)

	y.Source = "return {}"
	y.Method = "handle"
	y.Modules = []string{"json"}

	cmd := y.ToCommand()
	compileCmd, ok := cmd.(evalhost.CompileCmd)
	require.True(t, ok)
	assert.Equal(t, "return {}", compileCmd.Source)
	assert.Equal(t, "handle", compileCmd.Method)
	assert.Equal(t, []string{"json"}, compileCmd.Modules)
}

func TestCompileYield_CmdID(t *testing.T) {
	y := AcquireCompileYield()
	defer ReleaseCompileYield(y)

	assert.Equal(t, evalhost.Compile, y.CmdID())
}

func TestCreateProcessYield_Pool(t *testing.T) {
	y := AcquireCreateProcessYield()
	require.NotNil(t, y)

	// Set some values
	y.Clock = NewMockClock(0)

	ReleaseCreateProcessYield(y)

	y2 := AcquireCreateProcessYield()
	assert.Nil(t, y2.Program)
	assert.Nil(t, y2.Clock)
	assert.Nil(t, y2.Ctx)
	ReleaseCreateProcessYield(y2)
}

func TestCreateProcessYield_CmdID(t *testing.T) {
	y := AcquireCreateProcessYield()
	defer ReleaseCreateProcessYield(y)

	assert.Equal(t, evalhost.CreateProcess, y.CmdID())
}

func TestProgramWrapper(t *testing.T) {
	// Create a wrapper with nil program
	wrapper := &ProgramWrapper{program: nil}
	assert.Nil(t, wrapper.Program())
}
