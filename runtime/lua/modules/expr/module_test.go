package expr

import (
	"testing"

	lua "github.com/yuin/gopher-lua"
)

func TestBind(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	Bind(l)

	mod := l.GetGlobal("expr")
	if mod.Type() != lua.LTTable {
		t.Fatal("expr module not registered")
	}

	tbl := mod.(*lua.LTable)
	funcs := []string{"compile", "eval"}
	for _, fn := range funcs {
		if tbl.RawGetString(fn).Type() != lua.LTFunction {
			t.Errorf("%s function not registered", fn)
		}
	}
}

func TestEvalSimple(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local result, err = expr.eval("1 + 2")
		if err then
			error("unexpected error: " .. err)
		end
		if result ~= 3 then
			error("expected 3, got: " .. tostring(result))
		end
	`)
	if err != nil {
		t.Errorf("eval simple failed: %v", err)
	}
}

func TestEvalWithEnv(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local result, err = expr.eval("x + y", {x = 10, y = 20})
		if err then
			error("unexpected error: " .. err)
		end
		if result ~= 30 then
			error("expected 30, got: " .. tostring(result))
		end
	`)
	if err != nil {
		t.Errorf("eval with env failed: %v", err)
	}
}

func TestEvalBoolean(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local result, err = expr.eval("true && false")
		if err then
			error("unexpected error: " .. err)
		end
		if result ~= false then
			error("expected false, got: " .. tostring(result))
		end
	`)
	if err != nil {
		t.Errorf("eval boolean failed: %v", err)
	}
}

func TestEvalString(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local result, err = expr.eval('"hello" + " " + "world"')
		if err then
			error("unexpected error: " .. err)
		end
		if result ~= "hello world" then
			error("expected 'hello world', got: " .. tostring(result))
		end
	`)
	if err != nil {
		t.Errorf("eval string failed: %v", err)
	}
}

func TestEvalEmpty(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local result, err = expr.eval("")
		if result ~= nil then
			error("expected nil result for empty expression")
		end
		if err == nil then
			error("expected error for empty expression")
		end
	`)
	if err != nil {
		t.Errorf("eval empty failed: %v", err)
	}
}

func TestEvalInvalid(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local result, err = expr.eval("invalid syntax !!!")
		if result ~= nil then
			error("expected nil result for invalid expression")
		end
		if err == nil then
			error("expected error for invalid expression")
		end
	`)
	if err != nil {
		t.Errorf("eval invalid failed: %v", err)
	}
}

func TestCompileAndRun(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local program, err = expr.compile("a * b")
		if err then
			error("compile failed: " .. err)
		end
		if program == nil then
			error("program should not be nil")
		end

		local result, err = program:run({a = 5, b = 6})
		if err then
			error("run failed: " .. err)
		end
		if result ~= 30 then
			error("expected 30, got: " .. tostring(result))
		end
	`)
	if err != nil {
		t.Errorf("compile and run failed: %v", err)
	}
}

func TestCompileEmpty(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local program, err = expr.compile("")
		if program ~= nil then
			error("expected nil program for empty expression")
		end
		if err == nil then
			error("expected error for empty expression")
		end
	`)
	if err != nil {
		t.Errorf("compile empty failed: %v", err)
	}
}

func TestCompileInvalid(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local program, err = expr.compile("(((")
		if program ~= nil then
			error("expected nil program for invalid expression")
		end
		if err == nil then
			error("expected error for invalid expression")
		end
	`)
	if err != nil {
		t.Errorf("compile invalid failed: %v", err)
	}
}

func TestProgramRunWithoutEnv(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local program, err = expr.compile("2 + 2")
		if err then
			error("compile failed: " .. err)
		end

		local result, err = program:run()
		if err then
			error("run failed: " .. err)
		end
		if result ~= 4 then
			error("expected 4, got: " .. tostring(result))
		end
	`)
	if err != nil {
		t.Errorf("run without env failed: %v", err)
	}
}

func TestProgramRunMissingVar(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local program, err = expr.compile("x + y")
		if err then
			error("compile failed: " .. err)
		end

		local result, err = program:run({x = 10})
		if result ~= nil then
			error("expected nil result for missing variable")
		end
		if err == nil then
			error("expected error for missing variable")
		end
	`)
	if err != nil {
		t.Errorf("run missing var failed: %v", err)
	}
}

func TestCaching(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local result1, err1 = expr.eval("100 + 200")
		local result2, err2 = expr.eval("100 + 200")
		local result3, err3 = expr.eval("100 + 200")

		if err1 or err2 or err3 then
			error("unexpected error")
		end
		if result1 ~= 300 or result2 ~= 300 or result3 ~= 300 then
			error("caching test failed")
		end
	`)
	if err != nil {
		t.Errorf("caching test failed: %v", err)
	}
}

func TestCompileWithEnv(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local program, err = expr.compile("value * 2", {value = 0})
		if err then
			error("compile with env failed: " .. err)
		end

		local result, err = program:run({value = 50})
		if err then
			error("run failed: " .. err)
		end
		if result ~= 100 then
			error("expected 100, got: " .. tostring(result))
		end
	`)
	if err != nil {
		t.Errorf("compile with env failed: %v", err)
	}
}

func TestEvalComparison(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local result, err = expr.eval("x > 5", {x = 10})
		if err then
			error("unexpected error: " .. err)
		end
		if result ~= true then
			error("expected true, got: " .. tostring(result))
		end
	`)
	if err != nil {
		t.Errorf("eval comparison failed: %v", err)
	}
}

func TestEvalTernary(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local result, err = expr.eval('x > 0 ? "positive" : "negative"', {x = 5})
		if err then
			error("unexpected error: " .. err)
		end
		if result ~= "positive" then
			error("expected 'positive', got: " .. tostring(result))
		end
	`)
	if err != nil {
		t.Errorf("eval ternary failed: %v", err)
	}
}

func TestEvalBuiltinFunctions(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local result, err = expr.eval("max(1, 5, 3)")
		if err then
			error("unexpected error: " .. err)
		end
		if result ~= 5 then
			error("expected 5, got: " .. tostring(result))
		end
	`)
	if err != nil {
		t.Errorf("eval builtin functions failed: %v", err)
	}
}

func TestProgramMethods(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local program, err = expr.compile("1")
		if err then
			error("compile failed: " .. err)
		end
		if type(program.run) ~= "function" then
			error("run method not found")
		end
	`)
	if err != nil {
		t.Errorf("program methods failed: %v", err)
	}
}

func TestImmutability(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local success = pcall(function()
			expr.foo = "bar"
		end)
	`)
	if err != nil {
		t.Errorf("immutability test failed: %v", err)
	}
}

func TestEvalNil(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	Bind(l)

	err := l.DoString(`
		local result, err = expr.eval("nil")
		if err then
			error("unexpected error: " .. err)
		end
		if result ~= nil then
			error("expected nil")
		end
	`)
	if err != nil {
		t.Errorf("eval nil failed: %v", err)
	}
}
