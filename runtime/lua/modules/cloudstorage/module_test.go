// SPDX-License-Identifier: MPL-2.0

package cloudstorage

import (
	"errors"
	"strings"
	"testing"
	"time"

	lua "github.com/wippyai/go-lua"
	csapi "github.com/wippyai/runtime/api/cloudstorage"
)

func TestModuleLoads(t *testing.T) {
	mod, yields := Module.Build()

	if mod == nil {
		t.Fatal("expected module table to be non-nil")
	}

	if len(yields) != 7 {
		t.Errorf("expected 7 yield types, got %d", len(yields))
	}
}

func TestModuleHasGet(t *testing.T) {
	mod, _ := Module.Build()

	getFunc := mod.RawGetString("get")
	if getFunc == lua.LNil {
		t.Error("expected module to have 'get' function")
	}
}

func TestModuleIsImmutable(t *testing.T) {
	mod, _ := Module.Build()

	if !mod.Immutable {
		t.Error("expected module to be immutable")
	}
}

func TestYieldTypes(t *testing.T) {
	_, yields := Module.Build()

	expectedCmds := map[int]bool{
		int(csapi.ListObjects):     false,
		int(csapi.DownloadObject):  false,
		int(csapi.UploadObject):    false,
		int(csapi.DeleteObjects):   false,
		int(csapi.PresignedGetURL): false,
		int(csapi.PresignedPutURL): false,
		int(csapi.HeadObject):      false,
	}

	for _, y := range yields {
		cmdID := int(y.CmdID)
		if _, ok := expectedCmds[cmdID]; ok {
			expectedCmds[cmdID] = true
		}
	}

	for cmdID, found := range expectedCmds {
		if !found {
			t.Errorf("missing yield type for command ID %d", cmdID)
		}
	}
}

func TestListObjectsYieldPool(t *testing.T) {
	y1 := AcquireListObjectsYield()
	if y1 == nil {
		t.Fatal("expected non-nil yield")
	}
	if y1.ListObjectsCmd == nil {
		t.Fatal("expected non-nil command")
	}

	ReleaseListObjectsYield(y1)

	y2 := AcquireListObjectsYield()
	if y2 == nil {
		t.Fatal("expected non-nil yield after release")
	}
	ReleaseListObjectsYield(y2)
}

func TestDownloadObjectYieldPool(t *testing.T) {
	y1 := AcquireDownloadObjectYield()
	if y1 == nil {
		t.Fatal("expected non-nil yield")
	}
	if y1.DownloadObjectCmd == nil {
		t.Fatal("expected non-nil command")
	}

	ReleaseDownloadObjectYield(y1)

	y2 := AcquireDownloadObjectYield()
	if y2 == nil {
		t.Fatal("expected non-nil yield after release")
	}
	ReleaseDownloadObjectYield(y2)
}

func TestUploadObjectYieldPool(t *testing.T) {
	y1 := AcquireUploadObjectYield()
	if y1 == nil {
		t.Fatal("expected non-nil yield")
	}
	if y1.UploadObjectCmd == nil {
		t.Fatal("expected non-nil command")
	}

	ReleaseUploadObjectYield(y1)

	y2 := AcquireUploadObjectYield()
	if y2 == nil {
		t.Fatal("expected non-nil yield after release")
	}
	ReleaseUploadObjectYield(y2)
}

func TestDeleteObjectsYieldPool(t *testing.T) {
	y1 := AcquireDeleteObjectsYield()
	if y1 == nil {
		t.Fatal("expected non-nil yield")
	}
	if y1.DeleteObjectsCmd == nil {
		t.Fatal("expected non-nil command")
	}

	ReleaseDeleteObjectsYield(y1)

	y2 := AcquireDeleteObjectsYield()
	if y2 == nil {
		t.Fatal("expected non-nil yield after release")
	}
	ReleaseDeleteObjectsYield(y2)
}

func TestPresignedGetURLYieldPool(t *testing.T) {
	y1 := AcquirePresignedGetURLYield()
	if y1 == nil {
		t.Fatal("expected non-nil yield")
	}
	if y1.PresignedGetURLCmd == nil {
		t.Fatal("expected non-nil command")
	}

	ReleasePresignedGetURLYield(y1)

	y2 := AcquirePresignedGetURLYield()
	if y2 == nil {
		t.Fatal("expected non-nil yield after release")
	}
	ReleasePresignedGetURLYield(y2)
}

func TestPresignedPutURLYieldPool(t *testing.T) {
	y1 := AcquirePresignedPutURLYield()
	if y1 == nil {
		t.Fatal("expected non-nil yield")
	}
	if y1.PresignedPutURLCmd == nil {
		t.Fatal("expected non-nil command")
	}

	ReleasePresignedPutURLYield(y1)

	y2 := AcquirePresignedPutURLYield()
	if y2 == nil {
		t.Fatal("expected non-nil yield after release")
	}
	ReleasePresignedPutURLYield(y2)
}

func TestYieldStrings(t *testing.T) {
	tests := []struct {
		name     string
		yield    lua.LValue
		expected string
	}{
		{"ListObjects", AcquireListObjectsYield(), "<cloudstorage_list_objects_yield>"},
		{"DownloadObject", AcquireDownloadObjectYield(), "<cloudstorage_download_object_yield>"},
		{"UploadObject", AcquireUploadObjectYield(), "<cloudstorage_upload_object_yield>"},
		{"DeleteObjects", AcquireDeleteObjectsYield(), "<cloudstorage_delete_objects_yield>"},
		{"PresignedGetURL", AcquirePresignedGetURLYield(), "<cloudstorage_presigned_get_url_yield>"},
		{"PresignedPutURL", AcquirePresignedPutURLYield(), "<cloudstorage_presigned_put_url_yield>"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.yield.String() != tt.expected {
				t.Errorf("expected %s, got %s", tt.expected, tt.yield.String())
			}
		})
	}
}

func TestYieldTypes_LuaType(t *testing.T) {
	yields := []lua.LValue{
		AcquireListObjectsYield(),
		AcquireDownloadObjectYield(),
		AcquireUploadObjectYield(),
		AcquireDeleteObjectsYield(),
		AcquirePresignedGetURLYield(),
		AcquirePresignedPutURLYield(),
	}

	for _, y := range yields {
		if y.Type() != lua.LTUserData {
			t.Errorf("expected LTUserData, got %v for %s", y.Type(), y.String())
		}
	}
}

func TestListObjectsYieldHandleResult(t *testing.T) {
	now := time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		data    any
		err     error
		name    string
		wantErr bool
	}{
		{
			name: "success",
			data: csapi.ListObjectsResponse{
				Result: &csapi.ListObjectsResult{
					Objects: []csapi.ObjectMetadata{
						{
							Key:          "test.txt",
							Size:         100,
							ContentType:  "text/plain",
							ETag:         "etag1",
							StorageClass: "STANDARD",
							LastModified: now,
							Owner:        &csapi.Owner{ID: "owner-id", DisplayName: "Owner Name"},
							VersionID:    "v1",
						},
					},
					IsTruncated:           false,
					NextContinuationToken: "",
				},
				Error: nil,
			},
			err:     nil,
			wantErr: false,
		},
		{
			name:    "error",
			data:    nil,
			err:     errors.New("list failed"),
			wantErr: true,
		},
		{
			name:    "invalid response type",
			data:    "invalid",
			err:     nil,
			wantErr: true,
		},
		{
			name: "response with error",
			data: csapi.ListObjectsResponse{
				Error: errors.New("operation error"),
			},
			err:     nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := lua.NewState()
			defer l.Close()

			y := AcquireListObjectsYield()
			defer ReleaseListObjectsYield(y)

			result := y.HandleResult(l, tt.data, tt.err)

			if len(result) != 2 {
				t.Fatalf("expected 2 return values, got %d", len(result))
			}

			if tt.wantErr {
				if result[1] == lua.LNil {
					t.Error("expected error, got nil")
				}
			}
		})
	}
}

func TestDownloadObjectYieldHandleResult(t *testing.T) {
	tests := []struct {
		data    any
		err     error
		name    string
		wantErr bool
	}{
		{
			name:    "success",
			data:    csapi.DownloadObjectResponse{Error: nil},
			err:     nil,
			wantErr: false,
		},
		{
			name:    "error",
			data:    nil,
			err:     errors.New("download failed"),
			wantErr: true,
		},
		{
			name:    "invalid response type",
			data:    "invalid",
			err:     nil,
			wantErr: true,
		},
		{
			name:    "response with error",
			data:    csapi.DownloadObjectResponse{Error: errors.New("operation error")},
			err:     nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := lua.NewState()
			defer l.Close()

			y := AcquireDownloadObjectYield()
			defer ReleaseDownloadObjectYield(y)

			result := y.HandleResult(l, tt.data, tt.err)

			if tt.wantErr {
				if len(result) != 2 {
					t.Fatalf("expected 2 return values for error, got %d", len(result))
				}
				if result[1] == lua.LNil {
					t.Error("expected error, got nil")
				}
			}
		})
	}
}

func TestUploadObjectYieldHandleResult(t *testing.T) {
	tests := []struct {
		data    any
		err     error
		name    string
		wantErr bool
	}{
		{
			name:    "success",
			data:    csapi.UploadObjectResponse{Error: nil},
			err:     nil,
			wantErr: false,
		},
		{
			name:    "error",
			data:    nil,
			err:     errors.New("upload failed"),
			wantErr: true,
		},
		{
			name:    "invalid response type",
			data:    "invalid",
			err:     nil,
			wantErr: true,
		},
		{
			name:    "response with error",
			data:    csapi.UploadObjectResponse{Error: errors.New("operation error")},
			err:     nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := lua.NewState()
			defer l.Close()

			y := AcquireUploadObjectYield()
			defer ReleaseUploadObjectYield(y)

			result := y.HandleResult(l, tt.data, tt.err)

			if len(result) != 2 {
				t.Fatalf("expected 2 return values, got %d", len(result))
			}

			if tt.wantErr {
				if result[1] == lua.LNil {
					t.Error("expected error, got nil")
				}
			}
		})
	}
}

func TestUploadObjectYieldToCommand(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	y := AcquireUploadObjectYield()
	defer ReleaseUploadObjectYield(y)

	y.Content = lua.LString("test content")
	cmd := y.ToCommand()

	if cmd == nil {
		t.Fatal("expected non-nil command")
	}
}

func TestUploadObjectYieldToCommandUserData(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	y := AcquireUploadObjectYield()
	defer ReleaseUploadObjectYield(y)

	reader := strings.NewReader("test")
	ud := l.NewUserData()
	ud.Value = reader
	y.Content = ud

	cmd := y.ToCommand()

	if cmd == nil {
		t.Fatal("expected non-nil command")
	}
}

func TestDeleteObjectsYieldHandleResult(t *testing.T) {
	tests := []struct {
		data    any
		err     error
		name    string
		wantErr bool
	}{
		{
			name:    "success",
			data:    csapi.DeleteObjectsResponse{Error: nil},
			err:     nil,
			wantErr: false,
		},
		{
			name:    "error",
			data:    nil,
			err:     errors.New("delete failed"),
			wantErr: true,
		},
		{
			name:    "invalid response type",
			data:    "invalid",
			err:     nil,
			wantErr: true,
		},
		{
			name:    "response with error",
			data:    csapi.DeleteObjectsResponse{Error: errors.New("operation error")},
			err:     nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := lua.NewState()
			defer l.Close()

			y := AcquireDeleteObjectsYield()
			defer ReleaseDeleteObjectsYield(y)

			result := y.HandleResult(l, tt.data, tt.err)

			if len(result) != 2 {
				t.Fatalf("expected 2 return values, got %d", len(result))
			}

			if tt.wantErr {
				if result[1] == lua.LNil {
					t.Error("expected error, got nil")
				}
			}
		})
	}
}

func TestPresignedGetURLYieldHandleResult(t *testing.T) {
	tests := []struct {
		data    any
		err     error
		name    string
		wantErr bool
	}{
		{
			name:    "success",
			data:    csapi.PresignedGetURLResponse{URL: "https://example.com", Error: nil},
			err:     nil,
			wantErr: false,
		},
		{
			name:    "error",
			data:    nil,
			err:     errors.New("presign failed"),
			wantErr: true,
		},
		{
			name:    "invalid response type",
			data:    "invalid",
			err:     nil,
			wantErr: true,
		},
		{
			name:    "response with error",
			data:    csapi.PresignedGetURLResponse{Error: errors.New("operation error")},
			err:     nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := lua.NewState()
			defer l.Close()

			y := AcquirePresignedGetURLYield()
			defer ReleasePresignedGetURLYield(y)

			result := y.HandleResult(l, tt.data, tt.err)

			if len(result) != 2 {
				t.Fatalf("expected 2 return values, got %d", len(result))
			}

			if tt.wantErr {
				if result[1] == lua.LNil {
					t.Error("expected error, got nil")
				}
			}
		})
	}
}

func TestPresignedPutURLYieldHandleResult(t *testing.T) {
	tests := []struct {
		data    any
		err     error
		name    string
		wantErr bool
	}{
		{
			name:    "success",
			data:    csapi.PresignedPutURLResponse{URL: "https://example.com", Error: nil},
			err:     nil,
			wantErr: false,
		},
		{
			name:    "error",
			data:    nil,
			err:     errors.New("presign failed"),
			wantErr: true,
		},
		{
			name:    "invalid response type",
			data:    "invalid",
			err:     nil,
			wantErr: true,
		},
		{
			name:    "response with error",
			data:    csapi.PresignedPutURLResponse{Error: errors.New("operation error")},
			err:     nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := lua.NewState()
			defer l.Close()

			y := AcquirePresignedPutURLYield()
			defer ReleasePresignedPutURLYield(y)

			result := y.HandleResult(l, tt.data, tt.err)

			if len(result) != 2 {
				t.Fatalf("expected 2 return values, got %d", len(result))
			}

			if tt.wantErr {
				if result[1] == lua.LNil {
					t.Error("expected error, got nil")
				}
			}
		})
	}
}

func TestStorageWrapper(t *testing.T) {
	w := &storageWrapper{
		released: false,
	}

	if w.released {
		t.Error("new wrapper should not be released")
	}
}

func TestModuleInfo(t *testing.T) {
	if Module.Name != "cloudstorage" {
		t.Errorf("expected name 'cloudstorage', got '%s'", Module.Name)
	}
	if Module.Description == "" {
		t.Error("module should have a description")
	}
	if len(Module.Class) == 0 {
		t.Error("module should have at least one class")
	}
}

func TestHeadObjectYieldPool(t *testing.T) {
	y1 := AcquireHeadObjectYield()
	if y1 == nil || y1.HeadObjectCmd == nil {
		t.Fatal("expected non-nil yield with command")
	}
	ReleaseHeadObjectYield(y1)

	y2 := AcquireHeadObjectYield()
	if y2 == nil {
		t.Fatal("expected non-nil yield after release")
	}
	ReleaseHeadObjectYield(y2)
}

func TestHeadObjectYieldHandleResult_Success(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	y := AcquireHeadObjectYield()
	defer ReleaseHeadObjectYield(y)

	now := time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC)
	data := csapi.HeadObjectResponse{
		Result: &csapi.HeadObjectResult{
			Size:               42,
			ETag:               "head-etag",
			ContentType:        "text/plain",
			CacheControl:       "max-age=60",
			ContentDisposition: "inline",
			ContentEncoding:    "identity",
			StorageClass:       "STANDARD",
			VersionID:          "v1",
			LastModified:       now,
			UserMetadata:       map[string]string{"env": "staging"},
		},
	}

	result := y.HandleResult(l, data, nil)
	if len(result) != 2 {
		t.Fatalf("expected 2 return values, got %d", len(result))
	}
	if result[1] != lua.LNil {
		t.Fatalf("expected nil error, got %v", result[1])
	}
	tbl, ok := result[0].(*lua.LTable)
	if !ok {
		t.Fatalf("expected table result, got %T", result[0])
	}

	if got := tbl.RawGetString("etag").String(); got != "head-etag" {
		t.Errorf("etag mismatch: got %q", got)
	}
	if got := lua.LVAsNumber(tbl.RawGetString("size")); got != 42 {
		t.Errorf("size mismatch: got %v", got)
	}
	if got := tbl.RawGetString("storage_class").String(); got != "STANDARD" {
		t.Errorf("storage_class mismatch: got %q", got)
	}
	if got := lua.LVAsNumber(tbl.RawGetString("last_modified")); int64(got) != now.Unix() {
		t.Errorf("last_modified mismatch: got %v", got)
	}
	meta, ok := tbl.RawGetString("metadata").(*lua.LTable)
	if !ok {
		t.Fatalf("expected metadata table")
	}
	if got := meta.RawGetString("env").String(); got != "staging" {
		t.Errorf("metadata.env mismatch: got %q", got)
	}
}

func TestHeadObjectYieldHandleResult_Error(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	y := AcquireHeadObjectYield()
	defer ReleaseHeadObjectYield(y)

	result := y.HandleResult(l, csapi.HeadObjectResponse{Error: errors.New("boom")}, nil)
	if len(result) != 2 {
		t.Fatalf("expected 2 return values, got %d", len(result))
	}
	if result[1] == lua.LNil {
		t.Fatal("expected error, got nil")
	}
}

func TestPreconditionFailedMapping(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	y := AcquireUploadObjectYield()
	defer ReleaseUploadObjectYield(y)

	result := y.HandleResult(l, csapi.UploadObjectResponse{Error: csapi.ErrPreconditionFailed}, nil)
	if len(result) != 2 {
		t.Fatalf("expected 2 return values")
	}
	luaErr, ok := result[1].(*lua.Error)
	if !ok {
		t.Fatalf("expected *lua.Error, got %T", result[1])
	}
	if luaErr.Kind() != lua.Conflict {
		t.Errorf("expected Conflict kind, got %s", luaErr.Kind())
	}
	if !strings.Contains(luaErr.Message, "precondition_failed") {
		t.Errorf("expected message to contain 'precondition_failed', got %q", luaErr.Message)
	}
}

func TestNotFoundMapping(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	t.Run("head_object", func(t *testing.T) {
		y := AcquireHeadObjectYield()
		defer ReleaseHeadObjectYield(y)

		result := y.HandleResult(l, csapi.HeadObjectResponse{Error: csapi.ErrNotFound}, nil)
		if len(result) != 2 {
			t.Fatalf("expected 2 return values")
		}
		luaErr, ok := result[1].(*lua.Error)
		if !ok {
			t.Fatalf("expected *lua.Error, got %T", result[1])
		}
		if luaErr.Kind() != lua.NotFound {
			t.Errorf("expected NotFound kind, got %s", luaErr.Kind())
		}
		if !strings.Contains(luaErr.Message, "not_found") {
			t.Errorf("expected message to contain 'not_found', got %q", luaErr.Message)
		}
	})

	t.Run("download_object", func(t *testing.T) {
		y := AcquireDownloadObjectYield()
		defer ReleaseDownloadObjectYield(y)

		result := y.HandleResult(l, csapi.DownloadObjectResponse{Error: csapi.ErrNotFound}, nil)
		if len(result) != 2 {
			t.Fatalf("expected 2 return values")
		}
		luaErr, ok := result[1].(*lua.Error)
		if !ok {
			t.Fatalf("expected *lua.Error, got %T", result[1])
		}
		if luaErr.Kind() != lua.NotFound {
			t.Errorf("expected NotFound kind, got %s", luaErr.Kind())
		}
	})
}

func TestListObjectsYieldHandleResult_Fields(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	y := AcquireListObjectsYield()
	defer ReleaseListObjectsYield(y)

	now := time.Date(2026, 5, 5, 12, 0, 0, 0, time.UTC)
	data := csapi.ListObjectsResponse{
		Result: &csapi.ListObjectsResult{
			Objects: []csapi.ObjectMetadata{
				{
					Key:          "f.txt",
					Size:         11,
					ETag:         "e",
					StorageClass: "STANDARD",
					LastModified: now,
					VersionID:    "v1",
					Owner:        &csapi.Owner{ID: "oid", DisplayName: "ON"},
				},
			},
		},
	}

	res := y.HandleResult(l, data, nil)
	tbl := res[0].(*lua.LTable)
	objs := tbl.RawGetString("objects").(*lua.LTable)
	first := objs.RawGetInt(1).(*lua.LTable)

	if first.RawGetString("storage_class").String() != "STANDARD" {
		t.Error("storage_class missing")
	}
	if int64(lua.LVAsNumber(first.RawGetString("last_modified"))) != now.Unix() {
		t.Error("last_modified missing")
	}
	if first.RawGetString("version_id").String() != "v1" {
		t.Error("version_id missing")
	}
	owner, ok := first.RawGetString("owner").(*lua.LTable)
	if !ok {
		t.Fatal("owner missing")
	}
	if owner.RawGetString("id").String() != "oid" {
		t.Error("owner.id missing")
	}
}
