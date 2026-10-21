// SPDX-License-Identifier: MPL-2.0

package logger

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/logs"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func TestDerivedLoggerUsesWriteContext(t *testing.T) {
	for _, derivation := range []struct {
		name   string
		source string
		fields map[string]any
	}{
		{"named", `logger:named("child")`, nil},
		{"with", `logger:with({component = "library"})`, map[string]any{"component": "library"}},
		{"chained", `logger:named("parent"):with({component = "library"}):named("child"):with({ready = true})`, map[string]any{"component": "library", "ready": true}},
	} {
		for _, binding := range []string{"no_context", "no_logger", "with_logger"} {
			t.Run(derivation.name+"/"+binding, func(t *testing.T) {
				l := lua.NewState()
				defer l.Close()
				val, _ := Module.BuildValue()
				l.SetGlobal(Module.Name, val)
				bindingCore, bindingOutput := observer.New(zap.DebugLevel)
				switch binding {
				case "no_context":
					l.RemoveContext()
				case "no_logger":
					l.SetContext(ctxapi.NewRootContext())
				case "with_logger":
					l.SetContext(logs.WithLogger(ctxapi.NewRootContext(), zap.New(bindingCore)))
				}
				require.NoError(t, l.DoString(`child = `+derivation.source))

				for _, invocation := range []string{"first", "second"} {
					core, output := observer.New(zap.DebugLevel)
					base := zap.New(core).Named(invocation).With(zap.String("invocation", invocation))
					l.SetContext(logs.WithLogger(ctxapi.NewRootContext(), base))
					require.NoError(t, l.DoString(`
						child:debug("debug", {message_field = "value"})
						child:info("info", {message_field = "value"})
						child:warn("warn", {message_field = "value"})
						child:error("error", {message_field = "value"})
					`))
					entries := output.All()
					require.Len(t, entries, 4)
					name := invocation
					switch derivation.name {
					case "named":
						name += ".child"
					case "chained":
						name += ".parent.child"
					}
					for i, entry := range entries {
						require.Equal(t, zapcore.Level(i-1), entry.Level)
						require.Equal(t, name, entry.LoggerName)
						fields := map[string]any{"invocation": invocation, "message_field": "value"}
						for key, value := range derivation.fields {
							fields[key] = value
						}
						require.Equal(t, fields, entry.ContextMap())
					}
					l.SetContext(context.Background())
					require.NoError(t, l.DoString(`child:warn("without logger")`))
					require.Equal(t, 4, output.Len())
				}
				require.Zero(t, bindingOutput.Len())
			})
		}
	}
}

func TestDerivedLoggerBranchesKeepFields(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	val, _ := Module.BuildValue()
	l.SetGlobal(Module.Name, val)
	core, output := observer.New(zap.InfoLevel)
	l.SetContext(logs.WithLogger(ctxapi.NewRootContext(), zap.New(core)))
	require.NoError(t, l.DoString(`
		local fields = {component = "library"}
		local parent = logger:with(fields):with({a = true}):with({b = true})
		local left = parent:with({branch = "left"}):named("left")
		local right = parent:with({branch = "right"}):named("right")
		fields.component = "changed"
		left:info("left", {call = true})
		right:info("right")
		parent:info("parent")
		logger:info("root")
	`))
	entries := output.All()
	require.Len(t, entries, 4)
	require.Equal(t, "left", entries[0].LoggerName)
	require.Equal(t, map[string]any{"component": "library", "a": true, "b": true, "branch": "left", "call": true}, entries[0].ContextMap())
	require.Equal(t, "right", entries[1].LoggerName)
	require.Equal(t, map[string]any{"component": "library", "a": true, "b": true, "branch": "right"}, entries[1].ContextMap())
	require.Empty(t, entries[2].LoggerName)
	require.Equal(t, map[string]any{"component": "library", "a": true, "b": true}, entries[2].ContextMap())
	require.Empty(t, entries[3].LoggerName)
	require.Empty(t, entries[3].ContextMap())
}

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
