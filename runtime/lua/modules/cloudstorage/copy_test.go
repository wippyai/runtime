// SPDX-License-Identifier: MPL-2.0

package cloudstorage

import (
	"testing"
	"time"

	lua "github.com/wippyai/go-lua"
	csapi "github.com/wippyai/runtime/api/cloudstorage"
	apierror "github.com/wippyai/runtime/api/error"
	runtimelua "github.com/wippyai/runtime/runtime/lua"
)

func callCopyObject(t *testing.T, args ...lua.LValue) (*lua.LState, int) {
	t.Helper()
	l, storage := newStorageCallState(t)
	l.Push(storage)
	for _, a := range args {
		l.Push(a)
	}
	return l, storageCopyObject(l)
}

func TestStorageCopyObject_ParsesOptions(t *testing.T) {
	l, storage := newStorageCallState(t)
	meta := l.CreateTable(0, 1)
	meta.RawSetString("k", lua.LString("v"))
	opts := l.CreateTable(0, 5)
	opts.RawSetString("metadata_directive", lua.LString("REPLACE"))
	opts.RawSetString("metadata", meta)
	opts.RawSetString("content_type", lua.LString("text/plain"))
	opts.RawSetString("if_match", lua.LString(`"etag"`))
	opts.RawSetString("if_none_match", lua.LString("*"))
	l.Push(storage)
	l.Push(lua.LString("src"))
	l.Push(lua.LString("dst"))
	l.Push(opts)

	if got := storageCopyObject(l); got != -1 {
		t.Fatalf("expected yield result, got %d", got)
	}
	yield, ok := l.Get(-1).(*CopyObjectYield)
	if !ok {
		t.Fatalf("expected CopyObjectYield, got %T", l.Get(-1))
	}
	defer ReleaseCopyObjectYield(yield)

	if yield.SrcKey != "src" || yield.DstKey != "dst" {
		t.Fatalf("keys = %q -> %q", yield.SrcKey, yield.DstKey)
	}
	o := yield.Options
	if o.MetadataDirective != csapi.MetadataDirectiveReplace || o.Metadata["k"] != "v" ||
		o.ContentType != "text/plain" || o.IfMatch != `"etag"` || o.IfNoneMatch != "*" {
		t.Fatalf("unexpected options %+v", o)
	}
}

func TestStorageCopyObject_WithoutOptions(t *testing.T) {
	l, returns := callCopyObject(t, lua.LString("src"), lua.LString("dst"))
	if returns != -1 {
		t.Fatalf("expected yield result, got %d", returns)
	}
	yield := l.Get(-1).(*CopyObjectYield)
	defer ReleaseCopyObjectYield(yield)
	if yield.Options != nil {
		t.Fatalf("options = %+v, want nil", yield.Options)
	}
}

func TestStorageCopyObject_RejectsInvalidArguments(t *testing.T) {
	t.Run("empty source", func(t *testing.T) {
		l, returns := callCopyObject(t, lua.LString(""), lua.LString("dst"))
		assertInvalidLuaResult(t, l, returns)
	})
	t.Run("empty destination", func(t *testing.T) {
		l, returns := callCopyObject(t, lua.LString("src"), lua.LString(""))
		assertInvalidLuaResult(t, l, returns)
	})
	t.Run("unknown directive", func(t *testing.T) {
		l, storage := newStorageCallState(t)
		opts := l.CreateTable(0, 1)
		opts.RawSetString("metadata_directive", lua.LString("copy"))
		l.Push(storage)
		l.Push(lua.LString("src"))
		l.Push(lua.LString("dst"))
		l.Push(opts)
		assertInvalidLuaResult(t, l, storageCopyObject(l))
	})
	t.Run("released storage", func(t *testing.T) {
		l := lua.NewState()
		defer l.Close()
		ud := l.NewUserData()
		ud.Value = &storageWrapper{released: true}
		l.Push(ud)
		l.Push(lua.LString("src"))
		l.Push(lua.LString("dst"))
		assertInvalidLuaResult(t, l, storageCopyObject(l))
	})
}

func TestCopyObjectYield_HandleResult(t *testing.T) {
	l := lua.NewState()
	defer l.Close()
	y := AcquireCopyObjectYield()
	defer ReleaseCopyObjectYield(y)

	modified := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	result := y.HandleResult(l, csapi.CopyObjectResponse{Result: &csapi.CopyResult{ETag: `"abc"`, LastModified: modified}}, nil)
	if len(result) != 2 || result[1] != lua.LNil {
		t.Fatalf("unexpected result %v", result)
	}
	tbl := result[0].(*lua.LTable)
	if got := tbl.RawGetString("etag").String(); got != `"abc"` {
		t.Errorf("etag = %q", got)
	}
	if got := lua.LVAsNumber(tbl.RawGetString("last_modified")); int64(got) != modified.Unix() {
		t.Errorf("last_modified = %v, want %d", got, modified.Unix())
	}
}

func TestCopyObjectYield_ErrorKinds(t *testing.T) {
	lua.SetErrorMetadataExtractor(runtimelua.ErrorMetadata)
	t.Cleanup(func() { lua.SetErrorMetadataExtractor(nil) })

	cases := []struct {
		err       error
		name      string
		kind      lua.Kind
		message   string
		retryable lua.Ternary
	}{
		{name: "missing source", err: providerError(apierror.NotFound, apierror.False, "NoSuchKey", csapi.ErrNotFound), kind: lua.NotFound, retryable: lua.TernaryFalse, message: "not_found"},
		{name: "precondition", err: providerError(apierror.Conflict, apierror.False, "PreconditionFailed", csapi.ErrPreconditionFailed), kind: lua.Conflict, retryable: lua.TernaryFalse, message: "precondition_failed"},
		{name: "permission", err: providerError(apierror.PermissionDenied, apierror.False, "AccessDenied", nil), kind: lua.PermissionDenied, retryable: lua.TernaryFalse},
		{name: "transport", err: providerError(apierror.Unavailable, apierror.True, "RequestSendError", nil), kind: lua.Unavailable, retryable: lua.TernaryTrue},
		{name: "unsupported", err: csapi.ErrCopyUnsupported, kind: lua.Unavailable, retryable: lua.TernaryFalse},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l := lua.NewState()
			defer l.Close()
			y := AcquireCopyObjectYield()
			defer ReleaseCopyObjectYield(y)

			result := y.HandleResult(l, csapi.CopyObjectResponse{Error: tc.err}, nil)
			luaErr, ok := result[1].(*lua.Error)
			if !ok {
				t.Fatalf("expected *lua.Error, got %T", result[1])
			}
			if luaErr.Kind() != tc.kind || luaErr.Retryable() != tc.retryable {
				t.Errorf("kind/retryable = %s/%s, want %s/%s", luaErr.Kind(), luaErr.Retryable(), tc.kind, tc.retryable)
			}
			if tc.message != "" && luaErr.Message != tc.message {
				t.Errorf("message = %q, want %q", luaErr.Message, tc.message)
			}
		})
	}
}
