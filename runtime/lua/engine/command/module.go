package command

import (
	"fmt"
	"github.com/ponyruntime/pony/runtime/lua/engine/channel"
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
	// Create module table
	mod := L.NewTable()

	// Register functions
	L.SetField(mod, "new", L.NewFunction(newCommandFunc))

	// Command methods
	commandMethods := map[string]lua.LGFunction{
		"response":    responseFunc,
		"error":       errorFunc,
		"is_complete": isCompleteFunc,
		"result":      resultFunc,
		"is_canceled": isCanceledFunc,
	}

	// Command metatable
	mt := L.NewTypeMetatable("command")
	L.SetField(mt, "__index", L.SetFuncs(L.NewTable(), commandMethods))

	// Register module
	L.Push(mod)
	return 1
}

// Wrap wraps a command into a Lua value
func Wrap(L *lua.LState, cmd *Command) lua.LValue {
	ud := L.NewUserData()
	ud.Value = cmd
	L.SetMetatable(ud, L.GetTypeMetatable("command"))
	return ud
}

// Constructor functions
func newCommandFunc(L *lua.LState) int {
	cmdType := Type(L.CheckString(1))
	if cmdType == "" {
		L.RaiseError("command type cannot be empty")
		return 0
	}

	// Get params table
	params := make([]lua.LValue, 0)
	if L.GetTop() > 1 {
		paramTable := L.CheckTable(2)
		paramTable.ForEach(func(key lua.LValue, value lua.LValue) {
			params = append(params, value)
		})
	}

	cmd, err := NewCommand(cmdType, params...)
	if err != nil {
		L.RaiseError("failed to create command: %v", err)
		return 0
	}

	cmdBus := GetCommandContext(L.Context())
	if cmdBus == nil {
		L.RaiseError("command context not found")
		return 0
	}

	cmdBus.Schedule(cmd)
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
	isCanceled := cmd.Err() == ErrCommandCanceled
	L.Push(lua.LBool(isCanceled))
	return 1
}

// Get command error if any
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

// Get command result
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

// String implements fmt.Stringer for Command
func (c *Command) String() string {
	return fmt.Sprintf("command{type=%s completed=%v}", c.cmdType, c.completed)
}

// Type implements lua.LValue interface
func (c *Command) Type() lua.LValueType {
	return lua.LTUserData
}
