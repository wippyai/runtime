package async

import (
	"errors"
	lua "github.com/yuin/gopher-lua"
)

// Func is our custom function format
type Func func(args []lua.LValue) Result

// Result represents possible outputs from async function
type Result struct {
	Values []lua.LValue
	Err    error
}

type FuncWrapper struct {
	fn   Func
	args []lua.LValue
}

func (f *FuncWrapper) Type() lua.LValueType {
	return lua.LTFunction
}

func (f *FuncWrapper) String() string {
	return "async.func"
}

// WrapAsync wraps our Func into Lua-compatible format
func WrapAsync(L *lua.LState, fn Func) {
	top := L.GetTop()
	args := make([]lua.LValue, top)

	// Capture all arguments
	for i := 1; i <= top; i++ {
		args[i-1] = L.Get(i)
	}

	L.Push(&FuncWrapper{fn: fn, args: args}) // in detached state, we expect layer to handle it
}

// Run runs the wrapped function and returns results/error
func (f *FuncWrapper) Run() Result {
	if f.fn == nil {
		return Result{Err: errors.New("function has already been executed")}
	}

	r := f.fn(f.args)
	f.fn = nil
	f.args = nil

	return r
}
