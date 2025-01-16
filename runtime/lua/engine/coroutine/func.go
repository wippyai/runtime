package coroutine

import (
	"errors"
	lua "github.com/yuin/gopher-lua"
)

// Func is our simplified function format that just returns a Result
type Func func() Result

// Result represents possible outputs from async function
type Result struct {
	Values []lua.LValue
	Err    error
}

type FuncWrapper struct {
	fn Func
}

func (f *FuncWrapper) Type() lua.LValueType {
	return lua.LTFunction
}

func (f *FuncWrapper) String() string {
	return "async.func"
}

// WrapCoroutine wraps our Func into Lua-compatible format
func WrapCoroutine(L *lua.LState, fn Func) {
	L.Push(&FuncWrapper{fn: fn})
}

// Run runs the wrapped function and returns results/error
func (f *FuncWrapper) Run() Result {
	if f.fn == nil {
		return Result{Err: errors.New("function has already been executed")}
	}

	r := f.fn()
	f.fn = nil
	return r
}
