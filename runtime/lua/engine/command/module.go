package command

import (
	"errors"
	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
	"github.com/ponyruntime/pony/runtime/lua/engine/value"
	lua "github.com/yuin/gopher-lua"
)

// Module represents command module
type Module struct{}

// NewCommandModule creates a new command module instance
func NewCommandModule() *Module {
	return &Module{}
}

// Name returns the module name
func (m *Module) Name() string {
	return "command"
}

// Loader registers the module functions
func (m *Module) Loader(L *lua.LState) int {
	mod := L.CreateTable(0, 1)
	mod.RawSetString("new", L.NewFunction(newCommandFunc))

	value.RegisterMethods(L, "command", map[string]lua.LGFunction{
		"response":    responseFunc,
		"error":       errorFunc,
		"is_complete": isCompleteFunc,
		"result":      resultFunc,
		"is_canceled": isCanceledFunc,
		"cancel":      cancelFunc,
	})

	L.Push(mod)
	return 1
}

// Wrap wraps a command into a Lua value
func Wrap(L *lua.LState, cmd *Command) lua.LValue {
	ud := L.NewUserData()
	ud.Value = cmd
	ud.Metatable = value.GetTypeMetatable(L, "command")
	return ud
}

// Constructor functions
func newCommandFunc(L *lua.LState) int {
	cmdType := Type(L.CheckString(1))
	if cmdType == "" {
		L.RaiseError("command type cannot be empty")
		return 0
	}

	// Spawn params table
	numArgs := L.GetTop() - 1 // -1 for cmdType
	params := make([]lua.LValue, numArgs)
	for i := 0; i < numArgs; i++ {
		params[i] = L.Get(i + 2) // +2 to skip function and cmdType
	}

	cmd, err := NewCommand(cmdType, params...)
	if err != nil {
		L.RaiseError("failed to create command: %v", err)
		return 0
	}

	if err := Schedule(L.Context(), cmd); err != nil {
		L.RaiseError("failed to schedule command: %v", err)
		return 0
	}

	L.Push(Wrap(L, cmd))
	return 1
}

// Command methods
func responseFunc(L *lua.LState) int {
	cmd := CheckCommand(L)
	L.Push(channel.Wrap(L, cmd.response))
	return 1
}

// Check if command was canceled
func isCanceledFunc(L *lua.LState) int {
	cmd := CheckCommand(L)
	isCanceled := errors.Is(cmd.Err(), ErrCommandCanceled)
	L.Push(lua.LBool(isCanceled))
	return 1
}

// Cancel a queue command
func cancelFunc(L *lua.LState) int {
	cmd := CheckCommand(L)
	cmd.Cancel()

	// Queue the canceled command for processing
	if err := Error(L.Context(), cmd, ErrCommandCanceled); err != nil {
		L.RaiseError("failed to queue command cancellation: %v", err)
		return 0
	}

	L.Push(lua.LBool(true))
	return 1
}

// Spawn command error if any
func errorFunc(L *lua.LState) int {
	cmd := CheckCommand(L)
	if err := cmd.Err(); err != nil {
		L.Push(lua.LString(err.Error()))
	} else {
		L.Push(lua.LNil)
	}
	return 1
}

// Check if command is complete
func isCompleteFunc(L *lua.LState) int {
	cmd := CheckCommand(L)
	L.Push(lua.LBool(cmd.IsComplete()))
	return 1
}

// Spawn command result
func resultFunc(L *lua.LState) int {
	cmd := CheckCommand(L)
	result, err := cmd.Result()
	if err != nil {
		// Return nil + error message if there was an error
		L.Push(lua.LNil)
		L.Push(lua.LString(err.Error()))
		return 2
	}
	// Return result + nil if successful
	L.Push(result)
	L.Push(lua.LNil)
	return 2
}

// CheckCommand checks if the first argument is a Command
func CheckCommand(L *lua.LState) *Command {
	ud := L.CheckUserData(1)
	if cmd, ok := ud.Value.(*Command); ok {
		return cmd
	}
	L.ArgError(1, "command expected")
	return nil
}
