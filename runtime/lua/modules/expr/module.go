package expr

import (
	"fmt"
	"sync"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"
	lru "github.com/wippyai/runtime/internal/cache"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	"github.com/wippyai/runtime/system/payload/lua"
	luavm "github.com/yuin/gopher-lua"
)

// Option defines a functional option for configuring the expr module
type Option func(*config)

// config holds the configuration for the expr module
type config struct {
	capacity int
}

// WithCapacity sets the LRU cache capacity
func WithCapacity(capacity int) Option {
	return func(c *config) {
		c.capacity = capacity
	}
}

// Module represents the expr Lua module
type Module struct {
	cache       *lru.Cache[string, *vm.Program]
	moduleTable *luavm.LTable
	once        sync.Once
}

// Program wraps a compiled expr program
type Program struct {
	program *vm.Program
}

// NewExprModule creates a new expr module with LRU cache
func NewExprModule(opts ...Option) *Module {
	cfg := &config{
		capacity: 1000, // default capacity
	}

	// Apply options
	for _, opt := range opts {
		opt(cfg)
	}

	cache := lru.New[string, *vm.Program](
		lru.WithCapacity(cfg.capacity),
	)

	return &Module{
		cache: cache,
	}
}

// Name returns the module's name
func (m *Module) Name() string {
	return "expr"
}

// Loader loads the module into the given Lua state
func (m *Module) Loader(l *luavm.LState) int {
	m.once.Do(func() {
		m.initModuleTable(l)
	})

	l.Push(m.moduleTable)
	return 1
}

// initModuleTable creates and initializes the module table once
func (m *Module) initModuleTable(l *luavm.LState) {
	// Register the Program type methods
	value.RegisterMethods(l, "expr.Program", map[string]luavm.LGFunction{
		"run": m.programRun,
	})

	// Create module table
	t := l.CreateTable(0, 2) // 2 functions: compile, eval

	t.RawSetString("compile", l.NewFunction(m.luaCompile))
	t.RawSetString("eval", l.NewFunction(m.luaEval))

	// Make the table immutable so it can be safely reused
	t.Immutable = true

	m.moduleTable = t
}

// CheckProgram checks if the first argument is a Program and returns it
func CheckProgram(l *luavm.LState) *Program {
	ud := l.CheckUserData(1)
	if program, ok := ud.Value.(*Program); ok {
		return program
	}
	l.ArgError(1, "expected expr.Program")
	return nil
}

// WrapProgram wraps a Program as a Lua userdata
func WrapProgram(l *luavm.LState, program *Program) *luavm.LUserData {
	ud := l.NewUserData()
	ud.Value = program
	ud.Metatable = value.GetTypeMetatable(l, "expr.Program")
	return ud
}

// luaCompile compiles an expression without caching
func (m *Module) luaCompile(l *luavm.LState) int {
	// Get expression string
	expression := l.CheckString(1)
	if expression == "" {
		l.Push(luavm.LNil)
		l.Push(luavm.LString("expression cannot be empty"))
		return 2
	}

	// Get optional environment
	var env any
	if l.GetTop() >= 2 && l.Get(2) != luavm.LNil {
		env = lua.ToGoAny(l.Get(2))
	}

	// Compile expression
	program, err := m.compileExpression(expression, env)
	if err != nil {
		l.Push(luavm.LNil)
		l.Push(luavm.LString(err.Error()))
		return 2
	}

	// Wrap program in our Program type
	wrappedProgram := &Program{program: program}
	ud := WrapProgram(l, wrappedProgram)
	l.Push(ud)
	l.Push(luavm.LNil)
	return 2
}

// luaEval evaluates an expression with caching
func (m *Module) luaEval(l *luavm.LState) int {
	// Get expression string
	expression := l.CheckString(1)
	if expression == "" {
		l.Push(luavm.LNil)
		l.Push(luavm.LString("expression cannot be empty"))
		return 2
	}

	// Get optional environment
	var env any
	if l.GetTop() >= 2 && l.Get(2) != luavm.LNil {
		env = lua.ToGoAny(l.Get(2))
	}

	// Get or compile program from cache
	program, err := m.getCachedProgram(expression)
	if err != nil {
		l.Push(luavm.LNil)
		l.Push(luavm.LString(err.Error()))
		return 2
	}

	// Run program
	result, err := expr.Run(program, env)
	if err != nil {
		l.Push(luavm.LNil)
		l.Push(luavm.LString(err.Error()))
		return 2
	}

	// Convert result back to Lua
	luaResult, err := lua.GoToLua(result)
	if err != nil {
		l.Push(luavm.LNil)
		l.Push(luavm.LString(fmt.Sprintf("failed to convert result: %s", err.Error())))
		return 2
	}

	l.Push(luaResult)
	l.Push(luavm.LNil)
	return 2
}

// programRun runs a compiled program with environment
func (m *Module) programRun(l *luavm.LState) int {
	// Check and get program
	program := CheckProgram(l)
	if program == nil {
		return 0
	}

	// Get optional environment
	var env any
	if l.GetTop() >= 2 && l.Get(2) != luavm.LNil {
		env = lua.ToGoAny(l.Get(2))
	}

	// Run program
	result, err := expr.Run(program.program, env)
	if err != nil {
		l.Push(luavm.LNil)
		l.Push(luavm.LString(err.Error()))
		return 2
	}

	// Convert result back to Lua
	luaResult, err := lua.GoToLua(result)
	if err != nil {
		l.Push(luavm.LNil)
		l.Push(luavm.LString(fmt.Sprintf("failed to convert result: %s", err.Error())))
		return 2
	}

	l.Push(luaResult)
	l.Push(luavm.LNil)
	return 2
}

// getCachedProgram gets a compiled program from cache or compiles and caches it
func (m *Module) getCachedProgram(expression string) (*vm.Program, error) {
	// Try to get from cache first
	if program, found := m.cache.Get(expression); found {
		return program, nil
	}

	// Compile and cache
	program, err := m.compileExpression(expression, nil)
	if err != nil {
		return nil, err
	}

	// Cache the compiled program
	m.cache.Set(expression, program)
	return program, nil
}

// compileExpression compiles an expression (only built-ins supported)
func (m *Module) compileExpression(expression string, env any) (*vm.Program, error) {
	// Build options
	var options []expr.Option
	if env != nil {
		options = append(options, expr.Env(env))
	}

	// Compile expression (only built-in functions available)
	return expr.Compile(expression, options...)
}

// Close cleans up the module resources
func (m *Module) Close() {
	if m.cache != nil {
		m.cache.Close()
	}
}
