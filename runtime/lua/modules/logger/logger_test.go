package logger

import (
	"bytes"
	"testing"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/logs"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func TestLoad(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	val, _ := Module.BuildValue()
	l.SetGlobal(Module.Name, val)

	mod := l.GetGlobal("logger")
	if mod.Type() != lua.LTUserData {
		t.Fatalf("expected userdata, got %s", mod.Type())
	}
}

func TestLoadReuse(t *testing.T) {
	l1 := lua.NewState()
	defer l1.Close()
	l2 := lua.NewState()
	defer l2.Close()

	val, _ := Module.BuildValue()
	l1.SetGlobal(Module.Name, val)
	l2.SetGlobal(Module.Name, val)

	mod1 := l1.GetGlobal("logger").(*lua.LUserData)
	mod2 := l2.GetGlobal("logger").(*lua.LUserData)

	if mod1 != mod2 {
		t.Error("module userdata should be reused across states")
	}
}

func newTestLogger() (*zap.Logger, *bytes.Buffer) {
	buf := &bytes.Buffer{}
	encoder := zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig())
	core := zapcore.NewCore(encoder, zapcore.AddSync(buf), zapcore.DebugLevel)
	return zap.New(core), buf
}

func resetModule() {
	Module = &luaapi.ModuleDef{
		Name:        "logger",
		Description: "Structured logging",
		Class:       []string{luaapi.ClassIO},
		BuildValue:  buildModule,
	}
}

func TestModuleFunctions(t *testing.T) {
	log, buf := newTestLogger()
	resetModule()

	l := lua.NewState()
	defer l.Close()

	// Set context with logger using proper AppContext
	ctx := ctxapi.NewRootContext()
	logs.WithLogger(ctx, log)
	l.SetContext(ctx)

	val, _ := Module.BuildValue()
	l.SetGlobal(Module.Name, val)

	err := l.DoString(`
		logger:info("test message", {key = "value"})
	`)
	if err != nil {
		t.Fatalf("DoString failed: %v", err)
	}

	output := buf.String()
	if !bytes.Contains(buf.Bytes(), []byte("test message")) {
		t.Errorf("expected 'test message' in output, got: %s", output)
	}
	if !bytes.Contains(buf.Bytes(), []byte("value")) {
		t.Errorf("expected 'value' in output, got: %s", output)
	}
}

func TestLoggerWith(t *testing.T) {
	log, buf := newTestLogger()
	resetModule()

	l := lua.NewState()
	defer l.Close()

	ctx := ctxapi.NewRootContext()
	logs.WithLogger(ctx, log)
	l.SetContext(ctx)

	val, _ := Module.BuildValue()
	l.SetGlobal(Module.Name, val)

	err := l.DoString(`
		local child = logger:with({component = "test"})
		child:info("child message")
	`)
	if err != nil {
		t.Fatalf("DoString failed: %v", err)
	}

	output := buf.String()
	if !bytes.Contains(buf.Bytes(), []byte("child message")) {
		t.Errorf("expected 'child message' in output, got: %s", output)
	}
	if !bytes.Contains(buf.Bytes(), []byte("component")) {
		t.Errorf("expected 'component' field in output, got: %s", output)
	}
}

func TestLoggerNamed(t *testing.T) {
	log, buf := newTestLogger()
	resetModule()

	l := lua.NewState()
	defer l.Close()

	ctx := ctxapi.NewRootContext()
	logs.WithLogger(ctx, log)
	l.SetContext(ctx)

	val, _ := Module.BuildValue()
	l.SetGlobal(Module.Name, val)

	err := l.DoString(`
		local named = logger:named("mylogger")
		named:info("named message")
	`)
	if err != nil {
		t.Fatalf("DoString failed: %v", err)
	}

	output := buf.String()
	if !bytes.Contains(buf.Bytes(), []byte("named message")) {
		t.Errorf("expected 'named message' in output, got: %s", output)
	}
	if !bytes.Contains(buf.Bytes(), []byte("mylogger")) {
		t.Errorf("expected 'mylogger' in output, got: %s", output)
	}
}

func TestLogLevels(t *testing.T) {
	log, buf := newTestLogger()
	resetModule()

	l := lua.NewState()
	defer l.Close()

	ctx := ctxapi.NewRootContext()
	logs.WithLogger(ctx, log)
	l.SetContext(ctx)

	val, _ := Module.BuildValue()
	l.SetGlobal(Module.Name, val)

	err := l.DoString(`
		logger:debug("debug msg")
		logger:info("info msg")
		logger:warn("warn msg")
		logger:error("error msg")
	`)
	if err != nil {
		t.Fatalf("DoString failed: %v", err)
	}

	output := buf.String()
	if !bytes.Contains(buf.Bytes(), []byte("debug msg")) {
		t.Errorf("expected 'debug msg' in output, got: %s", output)
	}
	if !bytes.Contains(buf.Bytes(), []byte("info msg")) {
		t.Errorf("expected 'info msg' in output, got: %s", output)
	}
	if !bytes.Contains(buf.Bytes(), []byte("warn msg")) {
		t.Errorf("expected 'warn msg' in output, got: %s", output)
	}
	if !bytes.Contains(buf.Bytes(), []byte("error msg")) {
		t.Errorf("expected 'error msg' in output, got: %s", output)
	}
}

func TestFieldTypes(t *testing.T) {
	log, buf := newTestLogger()
	resetModule()

	l := lua.NewState()
	defer l.Close()

	ctx := ctxapi.NewRootContext()
	logs.WithLogger(ctx, log)
	l.SetContext(ctx)

	val, _ := Module.BuildValue()
	l.SetGlobal(Module.Name, val)

	err := l.DoString(`
		logger:info("types test", {
			str = "hello",
			num = 42,
			float = 3.14,
			bool = true
		})
	`)
	if err != nil {
		t.Fatalf("DoString failed: %v", err)
	}

	output := buf.String()
	if !bytes.Contains(buf.Bytes(), []byte("hello")) {
		t.Errorf("expected string field in output, got: %s", output)
	}
	if !bytes.Contains(buf.Bytes(), []byte("42")) {
		t.Errorf("expected number field in output, got: %s", output)
	}
	if !bytes.Contains(buf.Bytes(), []byte("3.14")) {
		t.Errorf("expected float field in output, got: %s", output)
	}
	if !bytes.Contains(buf.Bytes(), []byte("true")) {
		t.Errorf("expected bool field in output, got: %s", output)
	}
}

func TestChainedLoggers(t *testing.T) {
	log, buf := newTestLogger()
	resetModule()

	l := lua.NewState()
	defer l.Close()

	ctx := ctxapi.NewRootContext()
	logs.WithLogger(ctx, log)
	l.SetContext(ctx)

	val, _ := Module.BuildValue()
	l.SetGlobal(Module.Name, val)

	err := l.DoString(`
		local child1 = logger:with({service = "api"})
		local child2 = child1:with({method = "GET"})
		child2:info("chained")
	`)
	if err != nil {
		t.Fatalf("DoString failed: %v", err)
	}

	output := buf.String()
	if !bytes.Contains(buf.Bytes(), []byte("service")) {
		t.Errorf("expected 'service' field in output, got: %s", output)
	}
	if !bytes.Contains(buf.Bytes(), []byte("method")) {
		t.Errorf("expected 'method' field in output, got: %s", output)
	}
}
