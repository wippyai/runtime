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
			error("expected body_size 5, got " .. tostring(test_response.body_size))
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

func TestParseFileOptions(t *testing.T) {
	t.Run("valid file with content", func(t *testing.T) {
		l := lua.NewState()
		defer l.Close()

		files := l.CreateTable(1, 0)
		file1 := l.CreateTable(0, 3)
		file1.RawSetString("name", lua.LString("document"))
		file1.RawSetString("filename", lua.LString("test.txt"))
		file1.RawSetString("content", lua.LString("hello world"))
		files.RawSetInt(1, file1)

		opts := l.CreateTable(0, 1)
		opts.RawSetString("files", files)

		parsed := parseOptions(l, 1)
		l.Push(opts)
		parsed = parseOptions(l, 1)

		if len(parsed.files) != 1 {
			t.Errorf("expected 1 file, got %d", len(parsed.files))
		}
		if parsed.files[0].fieldName != "document" {
			t.Errorf("expected fieldName 'document', got '%s'", parsed.files[0].fieldName)
		}
		if parsed.files[0].fileName != "test.txt" {
			t.Errorf("expected fileName 'test.txt', got '%s'", parsed.files[0].fileName)
		}
		if string(parsed.files[0].data) != "hello world" {
			t.Errorf("expected content 'hello world', got '%s'", parsed.files[0].data)
		}
		if parsed.files[0].contentType != "application/octet-stream" {
			t.Errorf("expected default contentType 'application/octet-stream', got '%s'", parsed.files[0].contentType)
		}
	})

	t.Run("file without name is skipped", func(t *testing.T) {
		l := lua.NewState()
		defer l.Close()

		files := l.CreateTable(1, 0)
		file1 := l.CreateTable(0, 2)
		file1.RawSetString("filename", lua.LString("test.txt"))
		file1.RawSetString("content", lua.LString("hello"))
		files.RawSetInt(1, file1)

		opts := l.CreateTable(0, 1)
		opts.RawSetString("files", files)
		l.Push(opts)

		parsed := parseOptions(l, 1)
		if len(parsed.files) != 0 {
			t.Errorf("expected 0 files (invalid file should be skipped), got %d", len(parsed.files))
		}
	})

	t.Run("file without content is skipped", func(t *testing.T) {
		l := lua.NewState()
		defer l.Close()

		files := l.CreateTable(1, 0)
		file1 := l.CreateTable(0, 2)
		file1.RawSetString("name", lua.LString("document"))
		file1.RawSetString("filename", lua.LString("test.txt"))
		files.RawSetInt(1, file1)

		opts := l.CreateTable(0, 1)
		opts.RawSetString("files", files)
		l.Push(opts)

		parsed := parseOptions(l, 1)
		if len(parsed.files) != 0 {
			t.Errorf("expected 0 files (no content should be skipped), got %d", len(parsed.files))
		}
	})

	t.Run("multiple files", func(t *testing.T) {
		l := lua.NewState()
		defer l.Close()

		files := l.CreateTable(2, 0)

		file1 := l.CreateTable(0, 3)
		file1.RawSetString("name", lua.LString("file1"))
		file1.RawSetString("filename", lua.LString("doc1.txt"))
		file1.RawSetString("content", lua.LString("content1"))
		files.RawSetInt(1, file1)

		file2 := l.CreateTable(0, 3)
		file2.RawSetString("name", lua.LString("file2"))
		file2.RawSetString("filename", lua.LString("doc2.txt"))
		file2.RawSetString("content", lua.LString("content2"))
		files.RawSetInt(2, file2)

		opts := l.CreateTable(0, 1)
		opts.RawSetString("files", files)
		l.Push(opts)

		parsed := parseOptions(l, 1)
		if len(parsed.files) != 2 {
			t.Errorf("expected 2 files, got %d", len(parsed.files))
		}
	})

	t.Run("form data with files", func(t *testing.T) {
		l := lua.NewState()
		defer l.Close()

		form := l.CreateTable(0, 2)
		form.RawSetString("title", lua.LString("My Document"))
		form.RawSetString("description", lua.LString("A test file"))

		files := l.CreateTable(1, 0)
		file1 := l.CreateTable(0, 3)
		file1.RawSetString("name", lua.LString("attachment"))
		file1.RawSetString("filename", lua.LString("test.pdf"))
		file1.RawSetString("content", lua.LString("pdf content"))
		files.RawSetInt(1, file1)

		opts := l.CreateTable(0, 2)
		opts.RawSetString("form", form)
		opts.RawSetString("files", files)
		l.Push(opts)

		parsed := parseOptions(l, 1)
		if len(parsed.files) != 1 {
			t.Errorf("expected 1 file, got %d", len(parsed.files))
		}
		if parsed.form["title"] != "My Document" {
			t.Errorf("expected form title 'My Document', got '%s'", parsed.form["title"])
		}
	})

	t.Run("file with custom content_type", func(t *testing.T) {
		l := lua.NewState()
		defer l.Close()

		files := l.CreateTable(1, 0)
		file1 := l.CreateTable(0, 4)
		file1.RawSetString("name", lua.LString("image"))
		file1.RawSetString("filename", lua.LString("photo.jpg"))
		file1.RawSetString("content_type", lua.LString("image/jpeg"))
		file1.RawSetString("content", lua.LString("jpeg data"))
		files.RawSetInt(1, file1)

		opts := l.CreateTable(0, 1)
		opts.RawSetString("files", files)
		l.Push(opts)

		parsed := parseOptions(l, 1)
		if len(parsed.files) != 1 {
			t.Errorf("expected 1 file, got %d", len(parsed.files))
		}
		if parsed.files[0].contentType != "image/jpeg" {
			t.Errorf("expected contentType 'image/jpeg', got '%s'", parsed.files[0].contentType)
		}
	})

	t.Run("file with reader", func(t *testing.T) {
		l := lua.NewState()
		defer l.Close()

		reader := &mockReader{data: []byte("reader content")}
		ud := l.NewUserData()
		ud.Value = reader

		files := l.CreateTable(1, 0)
		file1 := l.CreateTable(0, 3)
		file1.RawSetString("name", lua.LString("document"))
		file1.RawSetString("filename", lua.LString("file.txt"))
		file1.RawSetString("reader", ud)
		files.RawSetInt(1, file1)

		opts := l.CreateTable(0, 1)
		opts.RawSetString("files", files)
		l.Push(opts)

		parsed := parseOptions(l, 1)
		if len(parsed.files) != 1 {
			t.Errorf("expected 1 file, got %d", len(parsed.files))
		}
		if parsed.files[0].reader == nil {
			t.Error("expected reader to be set")
		}
	})
}

type mockReader struct {
	data []byte
	pos  int
}

func (m *mockReader) Read(p []byte) (n int, err error) {
	if m.pos >= len(m.data) {
		return 0, nil
	}
	n = copy(p, m.data[m.pos:])
	m.pos += n
	if m.pos >= len(m.data) {
		err = nil
	}
	return n, err
}
