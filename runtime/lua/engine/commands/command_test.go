package commands

import (
	"github.com/stretchr/testify/assert"
	lua "github.com/yuin/gopher-lua"
	"testing"
)

func TestCommand_IsComplete(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	cmd, err := NewCommand("test", nil)
	if err != nil {
		t.Fatalf("Failed to create command: %v", err)
	}

	assert.False(t, cmd.IsComplete())

	cmd.SetResult(lua.LString("result"))
	assert.True(t, cmd.IsComplete())
}

func TestCommand_Result(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	cmd, err := NewCommand("test", nil)
	if err != nil {
		t.Fatalf("Failed to create command: %v", err)
	}

	// Test before completion
	result, err := cmd.Result()
	assert.Error(t, err)
	assert.Nil(t, result)

	// Test after successful completion
	expectedResult := lua.LString("success")
	cmd.SetResult(expectedResult)

	result, err = cmd.Result()
	assert.NoError(t, err)
	assert.Equal(t, expectedResult, result)
}

func TestCommand_SetError(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	cmd, err := NewCommand("test", nil)
	if err != nil {
		t.Fatalf("Failed to create command: %v", err)
	}

	testError := assert.AnError
	cmd.SetError(testError)

	assert.True(t, cmd.IsComplete())
	assert.Equal(t, testError, cmd.Err())

	result, err := cmd.Result()
	assert.Equal(t, testError, err)
	assert.Nil(t, result)
}

func TestCommand_Cancel(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	cmd, err := NewCommand("test", nil)
	if err != nil {
		t.Fatalf("Failed to create command: %v", err)
	}

	cmd.Cancel()

	assert.True(t, cmd.IsComplete())
	assert.Equal(t, ErrCommandCanceled, cmd.Err())

	result, err := cmd.Result()
	assert.Equal(t, ErrCommandCanceled, err)
	assert.Nil(t, result)
}

func TestCommandCounter(t *testing.T) {
	L := lua.NewState()
	defer L.Close()

	// Test that commands get unique channel names
	cmd1, err := NewCommand("test", nil)
	if err != nil {
		t.Fatalf("Failed to create first command: %v", err)
	}

	cmd2, err := NewCommand("test", nil)
	if err != nil {
		t.Fatalf("Failed to create second command: %v", err)
	}

	// Check that response channels are different
	assert.NotEqual(t, cmd1.response.Name(), cmd2.response.Name())
}
