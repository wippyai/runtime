package component

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	api "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/code"
	"github.com/wippyai/runtime/runtime/lua/engine"
	mocklogger "github.com/wippyai/runtime/tests/mock"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// testModule implements the api.Module interface for testing
type testModule struct {
	loader func(*lua.LState) int
	name   string
}

func (tm *testModule) Loader(l *lua.LState) int {
	return tm.loader(l)
}

func (tm *testModule) Name() string {
	return tm.name
}

func TestNewRunnerFactory(t *testing.T) {
	tests := []struct {
		name        string
		log         *zap.Logger
		compiled    *code.CompiledMain
		opts        []Option
		wantErr     bool
		expectedErr string
	}{
		{
			name:        "nil logger",
			log:         nil,
			compiled:    &code.CompiledMain{},
			wantErr:     true,
			expectedErr: "logger cannot be nil",
		},
		{
			name:        "nil compiled",
			log:         zap.NewNop(),
			compiled:    nil,
			wantErr:     true,
			expectedErr: "compiled code cannot be nil",
		},
		{
			name:     "valid basic factory",
			log:      zap.NewNop(),
			compiled: &code.CompiledMain{},
			wantErr:  false,
		},
		{
			name:     "factory with engine options",
			log:      zap.NewNop(),
			compiled: &code.CompiledMain{},
			opts: []Option{
				WithEngineOption(engine.WithGlobalValue("test", lua.LString("value"))),
			},
			wantErr: false,
		},
		{
			name:     "factory with global",
			log:      zap.NewNop(),
			compiled: &code.CompiledMain{},
			opts: []Option{
				WithGlobal("test_global", lua.LString("global_value")),
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			factory, err := NewRunnerFactory(tt.log, tt.compiled, tt.opts...)

			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, factory)
				if tt.expectedErr != "" {
					assert.Contains(t, err.Error(), tt.expectedErr)
				}
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, factory)
				assert.Equal(t, tt.log, factory.log)
				assert.Equal(t, tt.compiled, factory.compiled)
			}
		})
	}
}

func TestRunnerFactory_Compile(t *testing.T) {
	logger, _ := mocklogger.ZapTestLogger(zapcore.DebugLevel)

	// Create a simple function prototype for testing
	L := lua.NewState()
	defer L.Close()

	fn, err := L.LoadString("function main() return 'test' end")
	require.NoError(t, err)

	compiled := &code.CompiledMain{
		Main:     fn.Proto,
		FuncName: "main",
	}

	factory, err := NewRunnerFactory(logger, compiled)
	require.NoError(t, err)
	require.NotNil(t, factory)

	err = factory.Compile()
	assert.NoError(t, err)
}

func TestRunnerFactory_CreateVM(t *testing.T) {
	logger, _ := mocklogger.ZapTestLogger(zapcore.DebugLevel)

	// Create a simple function prototype for testing
	L := lua.NewState()
	defer L.Close()

	fn, err := L.LoadString("function main() return 'test' end")
	require.NoError(t, err)

	compiled := &code.CompiledMain{
		Main:     fn.Proto,
		FuncName: "main",
	}

	factory, err := NewRunnerFactory(logger, compiled)
	require.NoError(t, err)
	require.NotNil(t, factory)

	vm, err := factory.CreateVM()
	assert.NoError(t, err)
	assert.NotNil(t, vm)
	assert.Implements(t, (*api.VM)(nil), vm)
}

func TestRunnerFactory_CreateRunner(t *testing.T) {
	logger, _ := mocklogger.ZapTestLogger(zapcore.DebugLevel)

	// Create a simple function prototype for testing
	L := lua.NewState()
	defer L.Close()

	fn, err := L.LoadString("function main() return 'test' end")
	require.NoError(t, err)

	compiled := &code.CompiledMain{
		Main:     fn.Proto,
		FuncName: "main",
	}

	factory, err := NewRunnerFactory(logger, compiled)
	require.NoError(t, err)
	require.NotNil(t, factory)

	runner, err := factory.CreateRunner()
	assert.NoError(t, err)
	assert.NotNil(t, runner)
	assert.IsType(t, &engine.Runner{}, runner)
}

func TestRunnerFactory_CreateRunnerWithDependencies(t *testing.T) {
	logger, _ := mocklogger.ZapTestLogger(zapcore.DebugLevel)

	// Create a module for testing
	testModule := &testModule{
		loader: func(l *lua.LState) int {
			l.Push(lua.LString("module loaded"))
			return 1
		},
		name: "test_module",
	}

	// Create a simple function prototype for testing
	L := lua.NewState()
	defer L.Close()

	fn, err := L.LoadString("function main() return 'test' end")
	require.NoError(t, err)

	compiled := &code.CompiledMain{
		Main:     fn.Proto,
		FuncName: "main",
		Dependencies: []code.CompiledProto{
			{
				Name: "test_module",
				Node: &code.Node{
					Kind:   api.KindModule,
					Module: testModule,
				},
			},
		},
	}

	factory, err := NewRunnerFactory(logger, compiled)
	require.NoError(t, err)
	require.NotNil(t, factory)

	runner, err := factory.CreateRunner()
	assert.NoError(t, err)
	assert.NotNil(t, runner)
}

func TestRunnerFactory_CreateRunnerWithLibrary(t *testing.T) {
	logger, _ := mocklogger.ZapTestLogger(zapcore.DebugLevel)

	// Create a simple function prototype for testing
	L := lua.NewState()
	defer L.Close()

	fn, err := L.LoadString("function main() return 'test' end")
	require.NoError(t, err)

	libFn, err := L.LoadString("function lib_func() return 'library' end")
	require.NoError(t, err)

	compiled := &code.CompiledMain{
		Main:     fn.Proto,
		FuncName: "main",
		Dependencies: []code.CompiledProto{
			{
				Name:  "test_lib",
				Proto: libFn.Proto,
				Node: &code.Node{
					Kind:   api.KindLibrary,
					Source: "function lib_func() return 'library' end",
				},
			},
		},
	}

	factory, err := NewRunnerFactory(logger, compiled)
	require.NoError(t, err)
	require.NotNil(t, factory)

	runner, err := factory.CreateRunner()
	assert.NoError(t, err)
	assert.NotNil(t, runner)
}

func TestRunnerFactory_CreateRunnerWithFunction(t *testing.T) {
	logger, _ := mocklogger.ZapTestLogger(zapcore.DebugLevel)

	// Create a simple function prototype for testing
	L := lua.NewState()
	defer L.Close()

	fn, err := L.LoadString("function main() return 'test' end")
	require.NoError(t, err)

	funcFn, err := L.LoadString("function test_func() return 'function' end")
	require.NoError(t, err)

	compiled := &code.CompiledMain{
		Main:     fn.Proto,
		FuncName: "main",
		Dependencies: []code.CompiledProto{
			{
				Name:  "test_func",
				Proto: funcFn.Proto,
				Node: &code.Node{
					Kind:   api.KindFunction,
					Source: "function test_func() return 'function' end",
				},
			},
		},
	}

	factory, err := NewRunnerFactory(logger, compiled)
	require.NoError(t, err)
	require.NotNil(t, factory)

	runner, err := factory.CreateRunner()
	assert.NoError(t, err)
	assert.NotNil(t, runner)
}

func TestRunnerFactory_CreateRunnerWithPreloaded(t *testing.T) {
	logger, _ := mocklogger.ZapTestLogger(zapcore.DebugLevel)

	testModule := &testModule{
		loader: func(l *lua.LState) int {
			l.Push(lua.LString("preloaded"))
			return 1
		},
		name: "preloaded_module",
	}

	// Create a simple function prototype for testing
	L := lua.NewState()
	defer L.Close()

	fn, err := L.LoadString("function main() return 'test' end")
	require.NoError(t, err)

	compiled := &code.CompiledMain{
		Main:     fn.Proto,
		FuncName: "main",
		Preloaded: []code.CompiledProto{
			{
				Name: "preloaded_module",
				Node: &code.Node{
					Module: testModule,
				},
			},
		},
	}

	factory, err := NewRunnerFactory(logger, compiled)
	require.NoError(t, err)
	require.NotNil(t, factory)

	runner, err := factory.CreateRunner()
	assert.NoError(t, err)
	assert.NotNil(t, runner)
}

func TestRunnerFactory_Close(t *testing.T) {
	logger, _ := mocklogger.ZapTestLogger(zapcore.DebugLevel)

	// Create a simple function prototype for testing
	L := lua.NewState()
	defer L.Close()

	fn, err := L.LoadString("function main() return 'test' end")
	require.NoError(t, err)

	compiled := &code.CompiledMain{
		Main:     fn.Proto,
		FuncName: "main",
	}

	factory, err := NewRunnerFactory(logger, compiled)
	require.NoError(t, err)
	require.NotNil(t, factory)

	err = factory.Close()
	assert.NoError(t, err)
}

func TestRunnerFactory_ConcurrentAccess(t *testing.T) {
	logger, _ := mocklogger.ZapTestLogger(zapcore.DebugLevel)

	// Create a simple function prototype for testing
	L := lua.NewState()
	defer L.Close()

	fn, err := L.LoadString("function main() return 'test' end")
	require.NoError(t, err)

	compiled := &code.CompiledMain{
		Main:     fn.Proto,
		FuncName: "main",
	}

	factory, err := NewRunnerFactory(logger, compiled)
	require.NoError(t, err)
	require.NotNil(t, factory)

	// Test concurrent access to CreateRunner
	done := make(chan bool, 5)
	for i := 0; i < 5; i++ {
		go func() {
			runner, err := factory.CreateRunner()
			assert.NoError(t, err)
			assert.NotNil(t, runner)
			done <- true
		}()
	}

	// Wait for all goroutines to complete
	for i := 0; i < 5; i++ {
		<-done
	}
}

func TestRunnerFactory_WithNilDependencies(t *testing.T) {
	logger, _ := mocklogger.ZapTestLogger(zapcore.DebugLevel)

	// Create a simple function prototype for testing
	L := lua.NewState()
	defer L.Close()

	fn, err := L.LoadString("function main() return 'test' end")
	require.NoError(t, err)

	compiled := &code.CompiledMain{
		Main:     fn.Proto,
		FuncName: "main",
		Dependencies: []code.CompiledProto{
			{
				Name: "nil_dep",
				Node: nil, // This should be handled gracefully
			},
		},
	}

	factory, err := NewRunnerFactory(logger, compiled)
	require.NoError(t, err)
	require.NotNil(t, factory)

	runner, err := factory.CreateRunner()
	assert.NoError(t, err)
	assert.NotNil(t, runner)
}

func TestRunnerFactory_WithNilModule(t *testing.T) {
	logger, _ := mocklogger.ZapTestLogger(zapcore.DebugLevel)

	// Create a simple function prototype for testing
	L := lua.NewState()
	defer L.Close()

	fn, err := L.LoadString("function main() return 'test' end")
	require.NoError(t, err)

	compiled := &code.CompiledMain{
		Main:     fn.Proto,
		FuncName: "main",
		Dependencies: []code.CompiledProto{
			{
				Name: "nil_module",
				Node: &code.Node{
					Kind:   api.KindModule,
					Module: nil, // This should be handled gracefully
				},
			},
		},
	}

	factory, err := NewRunnerFactory(logger, compiled)
	require.NoError(t, err)
	require.NotNil(t, factory)

	runner, err := factory.CreateRunner()
	assert.NoError(t, err)
	assert.NotNil(t, runner)
}
