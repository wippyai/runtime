package io

import (
	"testing"

	"github.com/wippyai/runtime/runtime/wasm/resource"
)

func TestErrorHost(t *testing.T) {
	t.Run("creates with shared resources", func(t *testing.T) {
		res := resource.NewInstanceResources()
		defer res.Close()

		host := NewErrorHost(res)

		if host.Resources() != res {
			t.Error("expected same resources instance")
		}
	})

	t.Run("info returns correct namespace", func(t *testing.T) {
		res := resource.NewInstanceResources()
		defer res.Close()

		host := NewErrorHost(res)
		info := host.Info()

		if info.Namespace != ErrorNamespace {
			t.Errorf("namespace = %s, want %s", info.Namespace, ErrorNamespace)
		}
	})

	t.Run("register returns functions", func(t *testing.T) {
		res := resource.NewInstanceResources()
		defer res.Close()

		host := NewErrorHost(res)
		reg := host.Register()

		if reg.Functions == nil {
			t.Error("expected functions")
		}

		expectedFuncs := []string{
			"[method]error.to-debug-string",
			"[resource-drop]error",
		}

		for _, name := range expectedFuncs {
			if _, ok := reg.Functions[name]; !ok {
				t.Errorf("missing function: %s", name)
			}
		}
	})

	t.Run("no yield types for sync error functions", func(t *testing.T) {
		res := resource.NewInstanceResources()
		defer res.Close()

		host := NewErrorHost(res)
		reg := host.Register()

		if len(reg.YieldTypes) != 0 {
			t.Errorf("yield types = %d, want 0", len(reg.YieldTypes))
		}
	})

	t.Run("CreateError adds to table", func(t *testing.T) {
		res := resource.NewInstanceResources()
		defer res.Close()

		host := NewErrorHost(res)
		handle := host.CreateError(ErrorCodeTimeout, "connection timed out")

		if handle == 0 {
			t.Fatal("expected non-zero handle")
		}

		got, ok := host.Errors().Get(handle)
		if !ok {
			t.Fatal("expected error in table")
		}
		if got.Code != ErrorCodeTimeout {
			t.Errorf("code = %d, want %d", got.Code, ErrorCodeTimeout)
		}
		if got.Message != "connection timed out" {
			t.Errorf("message = %s, want 'connection timed out'", got.Message)
		}

		if res.Len() != 1 {
			t.Errorf("resource count = %d, want 1", res.Len())
		}
	})

	t.Run("error implements dropper", func(t *testing.T) {
		res := resource.NewInstanceResources()
		defer res.Close()

		host := NewErrorHost(res)
		handle := host.CreateError(ErrorCodeAccess, "access denied")

		err, ok := host.Errors().Get(handle)
		if !ok {
			t.Fatal("expected error")
		}

		res.Table().Remove(handle)

		if err.Message != "" {
			t.Error("expected message cleared after Drop")
		}
	})

	t.Run("debug string returns message or default", func(t *testing.T) {
		tests := []struct {
			code    ErrorCode
			message string
			want    string
		}{
			{ErrorCodeTimeout, "custom timeout", "custom timeout"},
			{ErrorCodeTimeout, "", "timeout"},
			{ErrorCodeAccess, "", "access denied"},
			{ErrorCodeWouldBlock, "", "operation would block"},
			{ErrorCodeBrokenPipe, "", "broken pipe"},
			{ErrorCodeClosed, "", "closed"},
			{ErrorCodeUnknown, "", "unknown error"},
		}

		for _, tt := range tests {
			err := &IOError{Code: tt.code, Message: tt.message}
			got := err.DebugString()
			if got != tt.want {
				t.Errorf("DebugString() for code=%d msg=%q = %q, want %q",
					tt.code, tt.message, got, tt.want)
			}
		}
	})

	t.Run("close releases all errors", func(t *testing.T) {
		res := resource.NewInstanceResources()
		host := NewErrorHost(res)

		host.CreateError(ErrorCodeAccess, "error 1")
		host.CreateError(ErrorCodeTimeout, "error 2")

		if res.Len() != 2 {
			t.Errorf("resource count = %d, want 2", res.Len())
		}

		res.Close()

		if res.Len() != 0 {
			t.Errorf("resource count after close = %d, want 0", res.Len())
		}
	})
}

func TestErrorCodes(t *testing.T) {
	codes := []struct {
		code ErrorCode
		want string
	}{
		{ErrorCodeUnknown, "unknown error"},
		{ErrorCodeAccess, "access denied"},
		{ErrorCodeWouldBlock, "operation would block"},
		{ErrorCodeInvalidSeek, "invalid seek"},
		{ErrorCodeBrokenPipe, "broken pipe"},
		{ErrorCodeConnectionReset, "connection reset"},
		{ErrorCodeConnectionRefused, "connection refused"},
		{ErrorCodeNotConnected, "not connected"},
		{ErrorCodeTimeout, "timeout"},
		{ErrorCodeClosed, "closed"},
	}

	for _, tt := range codes {
		err := &IOError{Code: tt.code}
		got := err.DebugString()
		if got != tt.want {
			t.Errorf("code %d: DebugString() = %q, want %q", tt.code, got, tt.want)
		}
	}
}

func BenchmarkErrorCreate(b *testing.B) {
	res := resource.NewInstanceResources()
	defer res.Close()

	host := NewErrorHost(res)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h := host.CreateError(ErrorCodeTimeout, "test error")
		res.Table().Remove(h)
	}
}
