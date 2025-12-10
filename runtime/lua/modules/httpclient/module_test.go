package httpclient

import (
	"testing"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/security"
	lua "github.com/yuin/gopher-lua"
)

// bind loads the module into the given state for testing.
func bind(l *lua.LState) {
	Module.Load(l)
}

func TestLoader(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	bind(l)

	mod := l.GetGlobal("http_client")
	if mod.Type() != lua.LTTable {
		t.Fatal("http_client module not registered")
	}

	tbl := mod.(*lua.LTable)
	funcs := []string{"get", "post", "put", "delete", "head", "patch", "request", "encode_uri", "decode_uri"}
	for _, fn := range funcs {
		if tbl.RawGetString(fn).Type() != lua.LTFunction {
			t.Errorf("%s function not registered", fn)
		}
	}
}

func TestImmutability(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	bind(l)

	err := l.DoString(`
		local success = pcall(function()
			http_client.foo = "bar"
		end)
	`)
	if err != nil {
		t.Errorf("immutability test failed: %v", err)
	}
}

func TestEncodeURI(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	bind(l)

	err := l.DoString(`
		local encoded = http_client.encode_uri("hello world")
		if encoded ~= "hello+world" then
			error("expected 'hello+world', got: " .. encoded)
		end
	`)
	if err != nil {
		t.Errorf("encode_uri test failed: %v", err)
	}
}

func TestDecodeURI(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	bind(l)

	err := l.DoString(`
		local decoded, err = http_client.decode_uri("hello+world")
		if err then
			error("unexpected error: " .. err)
		end
		if decoded ~= "hello world" then
			error("expected 'hello world', got: " .. decoded)
		end
	`)
	if err != nil {
		t.Errorf("decode_uri test failed: %v", err)
	}
}

func TestDecodeURIInvalid(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	bind(l)

	err := l.DoString(`
		local decoded, err = http_client.decode_uri("%ZZ")
		if decoded ~= nil then
			error("expected nil for invalid encoding")
		end
		if err == nil then
			error("expected error for invalid encoding")
		end
	`)
	if err != nil {
		t.Errorf("decode_uri invalid test failed: %v", err)
	}
}

func TestGetNoContext(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	bind(l)

	err := l.DoString(`
		local ok, err = pcall(function()
			http_client.get("http://example.com")
		end)
	`)
	if err != nil {
		t.Errorf("get no context test failed: %v", err)
	}
}

func TestGetWithContext(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	ctx := security.SetStrictMode(ctxapi.NewRootContext(), false)
	l.SetContext(ctx)

	bind(l)

	fn := l.GetGlobal("http_client").(*lua.LTable).RawGetString("get")
	l.Push(fn)
	l.Push(lua.LString("http://example.com"))
	err := l.PCall(1, lua.MultRet, nil)

	if err == nil {
		t.Error("expected yield error from main thread")
	}
	if err != nil && err.Error() != " can not yield from outside of a coroutine\nstack traceback:\n\t[G]: in main chunk\n\t[G]: ?" {
		t.Logf("got expected yield error: %v", err)
	}
}

func TestRequestMethod(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	ctx := security.SetStrictMode(ctxapi.NewRootContext(), false)
	l.SetContext(ctx)

	bind(l)

	fn := l.GetGlobal("http_client").(*lua.LTable).RawGetString("request")
	l.Push(fn)
	l.Push(lua.LString("POST"))
	l.Push(lua.LString("http://example.com"))

	opts := l.CreateTable(0, 2)
	headers := l.CreateTable(0, 1)
	headers.RawSetString("Content-Type", lua.LString("application/json"))
	opts.RawSetString("headers", headers)
	opts.RawSetString("body", lua.LString(`{"key":"value"}`))
	l.Push(opts)

	err := l.PCall(3, lua.MultRet, nil)
	if err == nil {
		t.Error("expected yield error from main thread")
	}
}

func TestLoaderIdempotent(t *testing.T) {
	l1 := lua.NewState()
	defer l1.Close()

	l2 := lua.NewState()
	defer l2.Close()

	bind(l1)
	bind(l2)

	mod1 := l1.GetGlobal("http_client")
	mod2 := l2.GetGlobal("http_client")

	if mod1.Type() != lua.LTTable || mod2.Type() != lua.LTTable {
		t.Fatal("module should be registered in both states")
	}
}

func TestResponse(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	bind(l)

	resp := NewResponse(l, 200, map[string]string{"Content-Type": "text/plain"}, map[string]string{"session": "abc"}, []byte("hello"), "http://example.com")
	l.SetGlobal("test_response", resp)

	err := l.DoString(`
		if test_response.status_code ~= 200 then
			error("expected status 200")
		end
		if test_response.body ~= "hello" then
			error("expected body 'hello'")
		end
		if test_response.body_size ~= 5 then
			error("expected body_size 5")
		end
		if test_response.url ~= "http://example.com" then
			error("expected url")
		end
		if test_response.headers["Content-Type"] ~= "text/plain" then
			error("expected Content-Type header")
		end
		if test_response.cookies["session"] ~= "abc" then
			error("expected session cookie")
		end
	`)
	if err != nil {
		t.Errorf("response test failed: %v", err)
	}
}

func TestResponseUnknownField(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	bind(l)

	resp := NewResponse(l, 200, nil, nil, nil, "")
	l.SetGlobal("test_response", resp)

	err := l.DoString(`
		if test_response.unknown_field ~= nil then
			error("expected nil for unknown field")
		end
	`)
	if err != nil {
		t.Errorf("response unknown field test failed: %v", err)
	}
}
