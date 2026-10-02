// SPDX-License-Identifier: MPL-2.0

package eval

import (
	"encoding/hex"

	lua "github.com/wippyai/go-lua"
	apihost "github.com/wippyai/runtime/api/host"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
)

const programTypeName = "eval.Program"

// Program is a Lua handle to a compiled eval program. It carries identity
// only; the eval host owns the compiled code.
type Program struct {
	handle apihost.EvalProgram
}

var programMethods = map[string]lua.LGoFunc{
	"spawn": programSpawn,
	"evict": programEvict,
}

var programMetamethods = map[string]lua.LGoFunc{
	"__tostring": programString,
}

func newProgram(l *lua.LState, handle apihost.EvalProgram) lua.LValue {
	return value.NewTypedUserData(l, &Program{handle: handle}, programTypeName)
}

func programArg(l *lua.LState, idx int) (apihost.EvalProgram, bool) {
	ud, ok := l.Get(idx).(*lua.LUserData)
	if !ok {
		return apihost.EvalProgram{}, false
	}
	program, ok := ud.Value.(*Program)
	if !ok {
		return apihost.EvalProgram{}, false
	}
	return program.handle, true
}

// programSpawn is Program:spawn(options?) -> pid, err.
func programSpawn(l *lua.LState) int {
	program, ok := programArg(l, 1)
	if !ok {
		return pushError(l, lua.Invalid, "eval.Program expected")
	}
	return spawnProgram(l, "", program)
}

// programEvict is Program:evict() -> true, err.
func programEvict(l *lua.LState) int {
	program, ok := programArg(l, 1)
	if !ok {
		return pushError(l, lua.Invalid, "eval.Program expected")
	}
	return evict(l, program)
}

func programString(l *lua.LState) int {
	program, ok := programArg(l, 1)
	if !ok {
		l.ArgError(1, "eval.Program expected")
		return 0
	}
	l.Push(lua.LString("eval.Program(" + hex.EncodeToString(program.Key.SourceHash[:4]) + ")"))
	return 1
}
