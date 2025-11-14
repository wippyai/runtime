package command

import (
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	payloadmod "github.com/wippyai/runtime/runtime/lua/modules/payload"
	luaconv "github.com/wippyai/runtime/system/payload/lua"
	lua "github.com/yuin/gopher-lua"
)

// Module represents the command Lua module
type Module struct{}

// NewCommandModule creates a new command module
func NewCommandModule() *Module {
	return &Module{}
}

// Name returns the module name
func (m *Module) Name() string {
	return "command.Command"
}

// Loader registers the module functions
func (m *Module) Loader(l *lua.LState) int {
	RegisterCommand(l)

	// Create module table with new function
	mod := l.CreateTable(0, 1)
	mod.RawSetString("new", l.NewFunction(newCommandFunc))

	l.Push(mod)
	return 1
}

func RegisterCommand(l *lua.LState) {
	// Register command type methods
	value.RegisterTypeMethods(l, "command.Command", nil, map[string]lua.LGFunction{
		"response":    responseFunc,
		"is_complete": isCompleteFunc,
		"result":      resultFunc,
		"is_canceled": isCanceledFunc,
		"cancel":      cancelFunc,
	})
}

// newCommandFunc creates a new command from Lua
// Params: cmdType (string), ...(payloads)
func newCommandFunc(l *lua.LState) int {
	cmdType := l.CheckString(1)
	if cmdType == "" {
		l.RaiseError("command type cannot be empty")
		return 0
	}

	// Collect parameters
	numArgs := l.GetTop() - 1 // -1 for cmdType
	params := make([]payload.Payload, numArgs)
	for i := 0; i < numArgs; i++ {
		argVal := l.Get(i + 2)

		// Check if argument is already a payload wrapper
		if ud, ok := argVal.(*lua.LUserData); ok {
			if pw, ok := ud.Value.(*payloadmod.Wrapper); ok {
				params[i] = pw.Payload
				continue
			}
		}

		// Otherwise create a new payload
		params[i] = luaconv.ExportPayload(argVal)
	}

	// Create the command with cancellation handler
	cmd := NewCommand(l, cmdType, nil, params...)

	// Return command userdata
	ud := l.NewUserData()
	ud.Value = cmd
	ud.Metatable = value.GetTypeMetatable(l, "command")
	l.Push(ud)
	return 1
}

// responseFunc returns the response channel of a command
// Method: command:response()
// Returns: channel
func responseFunc(l *lua.LState) int {
	cmd := CheckCommand(l)
	l.Push(cmd.channelValue)
	return 1
}

// isCompleteFunc checks if a command is complete
// Method: command:is_complete()
// Returns: boolean
func isCompleteFunc(l *lua.LState) int {
	cmd := CheckCommand(l)
	l.Push(lua.LBool(cmd.isCompleted()))
	return 1
}

// resultFunc returns the result of a command
// Method: command:result()
// Returns: payload, error
func resultFunc(l *lua.LState) int {
	cmd := CheckCommand(l)
	result := cmd.Result()

	if result == nil {
		// Not completed yet
		l.Push(lua.LNil)
		l.Push(lua.LString("command not completed"))
		return 2
	}

	if result.Error != nil {
		// Error occurred
		l.Push(lua.LNil)
		l.Push(lua.LString(result.Error.Error()))
		return 2
	}

	// Success case
	if result.Value == nil {
		// Create an empty payload
		nullPayload := payload.NewPayload(nil, payload.Lua)
		payloadmod.PushPayload(l, nullPayload)
		l.Push(lua.LNil)
		return 2
	}

	// Always return payload wrapper for consistency
	payloadmod.PushPayload(l, result.Value)
	l.Push(lua.LNil)
	return 2
}

// isCanceledFunc checks if a command was canceled
// Method: command:is_canceled()
// Returns: boolean
func isCanceledFunc(l *lua.LState) int {
	cmd := CheckCommand(l)
	l.Push(lua.LBool(cmd.isCanceled()))
	return 1
}

// cancelFunc cancels a command
// Method: command:cancel()
// Returns: boolean, error
func cancelFunc(l *lua.LState) int {
	cmd := CheckCommand(l)
	err := cmd.Cancel()
	if err != nil {
		l.Push(lua.LBool(false))
		l.Push(lua.LString(err.Error()))
		return 2
	}
	l.Push(lua.LBool(true))
	l.Push(lua.LNil)
	return 2
}

// CheckCommand validates and returns the command from Lua stack
func CheckCommand(l *lua.LState) *Command {
	ud := l.CheckUserData(1)
	if cmd, ok := ud.Value.(*Command); ok {
		return cmd
	}
	l.ArgError(1, "command expected")
	return nil
}

func WrapCommand(l *lua.LState, cmd *Command) *lua.LUserData {
	ud := l.NewUserData()
	ud.Value = cmd
	ud.Metatable = value.GetTypeMetatable(l, "command.Command")
	return ud
}
