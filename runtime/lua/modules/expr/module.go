package expr

import (
	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	lru "github.com/wippyai/runtime/internal/cache"
	luaconv "github.com/wippyai/runtime/runtime/lua/engine/payload"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	lua "github.com/yuin/gopher-lua"
)

const typeProgramName = "expr.Program"

func init() {
	value.RegisterTypeMethods(nil, typeProgramName, nil, map[string]lua.LGFunction{
		"run": programRun,
	})
}

// Options for expr module configuration.
type Options struct {
	CacheCapacity int
}

// DefaultOptions returns default configuration.
func DefaultOptions() Options {
	return Options{
		CacheCapacity: 1000,
	}
}

// Module is the default expr module with default options.
var Module = NewModule(DefaultOptions())

// NewModule creates an expr module with given options.
func NewModule(opts Options) *luaapi.ModuleDef {
	if opts.CacheCapacity <= 0 {
		opts.CacheCapacity = 1000
	}

	cache := lru.New[string, *vm.Program](lru.WithCapacity(opts.CacheCapacity))

	return &luaapi.ModuleDef{
		Name:        "expr",
		Description: "Expression language evaluation",
		Class:       []string{luaapi.ClassDeterministic},
		Build: func() (*lua.LTable, []luaapi.YieldType) {
			mod := lua.CreateTable(0, 2)
			mod.RawSetString("compile", lua.LGoFunc(makeCompileFunc()))
			mod.RawSetString("eval", lua.LGoFunc(makeEvalFunc(cache)))
			mod.Immutable = true
			return mod, nil
		},
	}
}

// Program wraps a compiled expression.
type Program struct {
	program *vm.Program
}

func checkProgram(l *lua.LState, idx int) *Program {
	ud := l.CheckUserData(idx)
	if program, ok := ud.Value.(*Program); ok {
		return program
	}
	l.ArgError(idx, "expected expr.Program")
	return nil
}

func invalidError(l *lua.LState, msg string) int {
	err := lua.NewLuaError(l, msg).
		WithKind(lua.KindInvalid).
		WithRetryable(false)
	l.Push(lua.LNil)
	l.Push(err)
	return 2
}

func internalError(l *lua.LState, goErr error, context string) int {
	err := lua.WrapErrorWithLua(l, goErr, context).
		WithKind(lua.KindInternal).
		WithRetryable(false)
	l.Push(lua.LNil)
	l.Push(err)
	return 2
}

func makeCompileFunc() lua.LGFunction {
	return func(l *lua.LState) int {
		expression := l.CheckString(1)
		if expression == "" {
			return invalidError(l, "expression cannot be empty")
		}

		var env any
		if l.GetTop() >= 2 && l.Get(2) != lua.LNil {
			env = value.ToGoAny(l.Get(2))
		}

		program, err := compileExpression(expression, env)
		if err != nil {
			return internalError(l, err, "compile failed")
		}

		wrappedProgram := &Program{program: program}
		value.PushTypedUserData(l, wrappedProgram, typeProgramName)
		l.Push(lua.LNil)
		return 2
	}
}

func makeEvalFunc(cache *lru.Cache[string, *vm.Program]) lua.LGFunction {
	return func(l *lua.LState) int {
		expression := l.CheckString(1)
		if expression == "" {
			return invalidError(l, "expression cannot be empty")
		}

		var env any
		if l.GetTop() >= 2 && l.Get(2) != lua.LNil {
			env = value.ToGoAny(l.Get(2))
		}

		program, err := getCachedProgram(cache, expression)
		if err != nil {
			return internalError(l, err, "compile failed")
		}

		result, err := expr.Run(program, env)
		if err != nil {
			return internalError(l, err, "eval failed")
		}

		luaResult, err := luaconv.GoToLua(result)
		if err != nil {
			return internalError(l, err, "failed to convert result")
		}

		l.Push(luaResult)
		l.Push(lua.LNil)
		return 2
	}
}

func programRun(l *lua.LState) int {
	program := checkProgram(l, 1)
	if program == nil {
		return 0
	}

	var env any
	if l.GetTop() >= 2 && l.Get(2) != lua.LNil {
		env = value.ToGoAny(l.Get(2))
	}

	result, err := expr.Run(program.program, env)
	if err != nil {
		return internalError(l, err, "run failed")
	}

	luaResult, err := luaconv.GoToLua(result)
	if err != nil {
		return internalError(l, err, "failed to convert result")
	}

	l.Push(luaResult)
	l.Push(lua.LNil)
	return 2
}

func getCachedProgram(cache *lru.Cache[string, *vm.Program], expression string) (*vm.Program, error) {
	if cache == nil {
		return compileExpression(expression, nil)
	}

	if program, found := cache.Get(expression); found {
		return program, nil
	}

	program, err := compileExpression(expression, nil)
	if err != nil {
		return nil, err
	}

	cache.Set(expression, program)
	return program, nil
}

func compileExpression(expression string, env any) (*vm.Program, error) {
	var options []expr.Option
	if env != nil {
		options = append(options, expr.Env(env))
	}

	return expr.Compile(expression, options...)
}
