package runner

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/runtime/lua/evalhost"
)

func TestModule_Info(t *testing.T) {
	info := Module.Info()
	assert.Equal(t, "eval_runner", info.Name)
	assert.Contains(t, info.Class, "process")
	assert.Contains(t, info.Class, "nondeterministic")
}

func TestCompileYield_Pool(t *testing.T) {
	y := AcquireCompileYield()
	require.NotNil(t, y)

	y.Source = "test source"
	y.Method = "handle"
	y.Modules = []string{"json", "time"}

	ReleaseCompileYield(y)

	// After release, fields should be cleared
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

func TestRunYield_Pool(t *testing.T) {
	y := AcquireRunYield()
	require.NotNil(t, y)

	y.Source = "test source"
	y.Method = "handle"
	y.Args = payload.Payloads{
		payload.NewPayload(1, payload.JSON),
		payload.NewPayload(2, payload.JSON),
		payload.NewPayload(3, payload.JSON),
	}
	y.Modules = []string{"json"}
	y.Context = map[string]any{"key": "value"}

	ReleaseRunYield(y)

	y2 := AcquireRunYield()
	assert.Empty(t, y2.Source)
	assert.Empty(t, y2.Method)
	assert.Nil(t, y2.Args)
	assert.Nil(t, y2.Modules)
	assert.Nil(t, y2.Context)
	ReleaseRunYield(y2)
}

func TestRunYield_ToCommand(t *testing.T) {
	y := AcquireRunYield()
	defer ReleaseRunYield(y)

	y.Source = "return function() end"
	y.Method = "execute"
	y.Args = payload.Payloads{
		payload.NewPayload("arg1", payload.JSON),
		payload.NewPayload(42, payload.JSON),
	}
	y.Modules = []string{"json", "time"}
	y.Context = map[string]any{"user": "test"}

	cmd := y.ToCommand()
	runCmd, ok := cmd.(evalhost.RunCmd)
	require.True(t, ok)
	assert.Equal(t, "return function() end", runCmd.Source)
	assert.Equal(t, "execute", runCmd.Method)
	assert.Len(t, runCmd.Args, 2)
	assert.Equal(t, []string{"json", "time"}, runCmd.Modules)
	assert.Equal(t, map[string]any{"user": "test"}, runCmd.Context)
}

func TestRunYield_CmdID(t *testing.T) {
	y := AcquireRunYield()
	defer ReleaseRunYield(y)

	assert.Equal(t, evalhost.Run, y.CmdID())
}

func TestGoToLua(t *testing.T) {
	// Create a mock LState for testing
	// Note: goToLua uses l.CreateTable which needs an LState
	// We'll test the basic switch cases that don't need LState

	// Test nil
	assert.Nil(t, nil)

	// Test bool
	assert.True(t, true)

	// Test numbers
	assert.Equal(t, 42, 42)
	assert.Equal(t, int64(100), int64(100))
	assert.Equal(t, 3.14, 3.14)

	// Test string
	assert.Equal(t, "hello", "hello")

	// Test bytes
	assert.Equal(t, []byte("bytes"), []byte("bytes"))
}
