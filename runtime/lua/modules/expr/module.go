package expr

import (
	"fmt"
	"sync"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	lru "github.com/wippyai/runtime/internal/cache"
	luaconv "github.com/wippyai/runtime/runtime/lua/engine/payload"
	luavm "github.com/yuin/gopher-lua"
)

const programMetatable = "expr.Program"

var (
	cache        *lru.Cache[string, *vm.Program]
	moduleTable  *luavm.LTable
	programMT    *luavm.LTable
	registration *luaapi.Registration
	initOnce     sync.Once
)

// Module is the singleton expr module instance.
var Module = &exprModule{}

type exprModule struct{}

func (m *exprModule) Info() luaapi.ModuleInfo {
	return luaapi.ModuleInfo{
		Name:        "expr",
		Description: "Expression language evaluation",
		Class:       []string{luaapi.ClassDeterministic},
	}
}

func (m *exprModule) Register(l *luavm.LState) *luaapi.Registration {
	initOnce.Do(func() {
		cache = lru.New[string, *vm.Program](
			lru.WithCapacity(1000),
		)

		programMT = createProgramMetatable(l)

		mod := &luavm.LTable{}
		mod.RawSetString("compile", luavm.LGoFunc(luaCompile))
		mod.RawSetString("eval", luavm.LGoFunc(luaEval))
		mod.Immutable = true
		moduleTable = mod

		registration = &luaapi.Registration{
			Table:      moduleTable,
			YieldTypes: nil,
		}
	})

	l.SetField(l.Get(luavm.RegistryIndex), programMetatable, programMT)

	return registration
}

func (m *exprModule) Loader(l *luavm.LState) int {
	reg := m.Register(l)
	l.Push(reg.Table)
	return 1
}

// Bind is deprecated. Use luaapi.LoadModule(l, Module) instead.
func Bind(l *luavm.LState) {
	luaapi.LoadModule(l, Module)
}

type Program struct {
	program *vm.Program
}

func getProgramMT(l *luavm.LState) luavm.LValue {
	return l.GetField(l.Get(luavm.RegistryIndex), programMetatable)
}

func createProgramMetatable(l *luavm.LState) *luavm.LTable {
	mt := l.CreateTable(0, 2)

	index := l.CreateTable(0, 1)
	index.RawSetString("run", luavm.LGoFunc(programRun))
	index.Immutable = true

	mt.RawSetString("__index", index)
	mt.Immutable = true
	return mt
}

func checkProgram(l *luavm.LState) *Program {
	ud := l.CheckUserData(1)
	if program, ok := ud.Value.(*Program); ok {
		return program
	}
	l.ArgError(1, "expected expr.Program")
	return nil
}

func wrapProgram(l *luavm.LState, program *Program) *luavm.LUserData {
	ud := l.NewUserData()
	ud.Value = program
	ud.Metatable = getProgramMT(l)
	return ud
}

func luaCompile(l *luavm.LState) int {
	expression := l.CheckString(1)
	if expression == "" {
		l.Push(luavm.LNil)
		l.Push(luavm.LString("expression cannot be empty"))
		return 2
	}

	var env any
	if l.GetTop() >= 2 && l.Get(2) != luavm.LNil {
		env = toGoAny(l.Get(2))
	}

	program, err := compileExpression(expression, env)
	if err != nil {
		l.Push(luavm.LNil)
		l.Push(luavm.LString(err.Error()))
		return 2
	}

	wrappedProgram := &Program{program: program}
	ud := wrapProgram(l, wrappedProgram)
	l.Push(ud)
	l.Push(luavm.LNil)
	return 2
}

func luaEval(l *luavm.LState) int {
	expression := l.CheckString(1)
	if expression == "" {
		l.Push(luavm.LNil)
		l.Push(luavm.LString("expression cannot be empty"))
		return 2
	}

	var env any
	if l.GetTop() >= 2 && l.Get(2) != luavm.LNil {
		env = toGoAny(l.Get(2))
	}

	program, err := getCachedProgram(expression)
	if err != nil {
		l.Push(luavm.LNil)
		l.Push(luavm.LString(err.Error()))
		return 2
	}

	result, err := expr.Run(program, env)
	if err != nil {
		l.Push(luavm.LNil)
		l.Push(luavm.LString(err.Error()))
		return 2
	}

	luaResult, err := luaconv.GoToLua(result)
	if err != nil {
		l.Push(luavm.LNil)
		l.Push(luavm.LString(fmt.Sprintf("failed to convert result: %s", err.Error())))
		return 2
	}

	l.Push(luaResult)
	l.Push(luavm.LNil)
	return 2
}

func programRun(l *luavm.LState) int {
	program := checkProgram(l)
	if program == nil {
		return 0
	}

	var env any
	if l.GetTop() >= 2 && l.Get(2) != luavm.LNil {
		env = toGoAny(l.Get(2))
	}

	result, err := expr.Run(program.program, env)
	if err != nil {
		l.Push(luavm.LNil)
		l.Push(luavm.LString(err.Error()))
		return 2
	}

	luaResult, err := luaconv.GoToLua(result)
	if err != nil {
		l.Push(luavm.LNil)
		l.Push(luavm.LString(fmt.Sprintf("failed to convert result: %s", err.Error())))
		return 2
	}

	l.Push(luaResult)
	l.Push(luavm.LNil)
	return 2
}

func getCachedProgram(expression string) (*vm.Program, error) {
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

func toGoAny(lv luavm.LValue) any {
	switch v := lv.(type) {
	case luavm.LBool:
		return bool(v)
	case luavm.LNumber:
		return float64(v)
	case luavm.LInteger:
		return int64(v)
	case luavm.LString:
		return string(v)
	case *luavm.LTable:
		return tableToMap(v)
	case *luavm.LNilType:
		return nil
	default:
		return nil
	}
}

func tableToMap(tbl *luavm.LTable) map[string]any {
	result := make(map[string]any)
	tbl.ForEach(func(key luavm.LValue, val luavm.LValue) {
		if keyStr, ok := key.(luavm.LString); ok {
			result[string(keyStr)] = toGoAny(val)
		}
	})
	return result
}
