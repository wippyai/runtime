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

	if len(yields) != 12 {
		t.Errorf("expected 12 yield types, got %d", len(yields))
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
		int(csapi.ListObjects):             false,
		int(csapi.DownloadObject):          false,
		int(csapi.UploadObject):            false,
		int(csapi.DeleteObjects):           false,
		int(csapi.PresignedGetURL):         false,
		int(csapi.PresignedPutURL):         false,
		int(csapi.HeadObject):              false,
		int(csapi.CreateMultipartUpload):   false,
		int(csapi.PresignedUploadPartURLs): false,
		int(csapi.CompleteMultipartUpload): false,
		int(csapi.AbortMultipartUpload):    false,
		int(csapi.OpenReader):              false,
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
			Headers:            map[string]string{"x-amz-tagging-count": "1"},
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
	headers, ok := tbl.RawGetString("headers").(*lua.LTable)
	if !ok {
		t.Fatalf("expected headers table")
	}
	if got := headers.RawGetString("x-amz-tagging-count").String(); got != "1" {
		t.Errorf("headers[x-amz-tagging-count] mismatch: got %q", got)
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

func TestCreateMultipartUploadYieldPool(t *testing.T) {
	y1 := AcquireCreateMultipartUploadYield()
	if y1 == nil || y1.CreateMultipartUploadCmd == nil {
		t.Fatal("expected non-nil yield and command")
	}
	ReleaseCreateMultipartUploadYield(y1)

	y2 := AcquireCreateMultipartUploadYield()
	if y2 == nil || y2.CreateMultipartUploadCmd == nil {
		t.Fatal("expected non-nil yield after release")
	}
	ReleaseCreateMultipartUploadYield(y2)
}

func TestPresignedPartURLsYieldPool(t *testing.T) {
	y1 := AcquirePresignedPartURLsYield()
	if y1 == nil || y1.PresignedPartURLsCmd == nil {
		t.Fatal("expected non-nil yield and command")
	}
	ReleasePresignedPartURLsYield(y1)

	y2 := AcquirePresignedPartURLsYield()
	if y2 == nil || y2.PresignedPartURLsCmd == nil {
		t.Fatal("expected non-nil yield after release")
	}
	if y2.Expiration != 0 {
		t.Error("expiration not reset on release")
	}
	ReleasePresignedPartURLsYield(y2)
}

func TestCompleteMultipartUploadYieldPool(t *testing.T) {
	y1 := AcquireCompleteMultipartUploadYield()
	if y1 == nil || y1.CompleteMultipartUploadCmd == nil {
		t.Fatal("expected non-nil yield and command")
	}
	ReleaseCompleteMultipartUploadYield(y1)

	y2 := AcquireCompleteMultipartUploadYield()
	if y2 == nil || y2.CompleteMultipartUploadCmd == nil {
		t.Fatal("expected non-nil yield after release")
	}
	ReleaseCompleteMultipartUploadYield(y2)
}

func TestAbortMultipartUploadYieldPool(t *testing.T) {
	y1 := AcquireAbortMultipartUploadYield()
	if y1 == nil || y1.AbortMultipartUploadCmd == nil {
		t.Fatal("expected non-nil yield and command")
	}
	ReleaseAbortMultipartUploadYield(y1)

	y2 := AcquireAbortMultipartUploadYield()
	if y2 == nil || y2.AbortMultipartUploadCmd == nil {
		t.Fatal("expected non-nil yield after release")
	}
	ReleaseAbortMultipartUploadYield(y2)
}

func TestOpenReaderYieldPool(t *testing.T) {
	y1 := AcquireOpenReaderYield()
	if y1 == nil || y1.OpenReaderCmd == nil {
		t.Fatal("expected non-nil yield and command")
	}
	y1.BlockSize = 1024
	y1.CacheBlocks = 2
	ReleaseOpenReaderYield(y1)

	y2 := AcquireOpenReaderYield()
	if y2 == nil || y2.OpenReaderCmd == nil {
		t.Fatal("expected non-nil yield after release")
	}
	if y2.BlockSize != 0 || y2.CacheBlocks != 0 {
		t.Error("tuning fields not reset on release")
	}
	ReleaseOpenReaderYield(y2)
}

func TestCreateMultipartUploadYield_HandleResult(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	y := AcquireCreateMultipartUploadYield()
	defer ReleaseCreateMultipartUploadYield(y)

	res := y.HandleResult(l, csapi.CreateMultipartUploadResponse{
		Result: &csapi.CreateMultipartUploadResult{UploadID: "up-42"},
	}, nil)
	tbl, ok := res[0].(*lua.LTable)
	if !ok {
		t.Fatalf("expected table, got %T", res[0])
	}
	if tbl.RawGetString("upload_id").String() != "up-42" {
		t.Error("upload_id missing")
	}
}

func TestPresignedPartURLsYield_HandleResult(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	y := AcquirePresignedPartURLsYield()
	defer ReleasePresignedPartURLsYield(y)

	res := y.HandleResult(l, csapi.PresignedPartURLsResponse{
		URLs: []csapi.PresignedPartURL{
			{PartNumber: 1, URL: "https://p1"},
			{PartNumber: 7, URL: "https://p7"},
		},
	}, nil)
	arr, ok := res[0].(*lua.LTable)
	if !ok {
		t.Fatalf("expected table, got %T", res[0])
	}
	if arr.Len() != 2 {
		t.Fatalf("expected 2 urls, got %d", arr.Len())
	}
	second := arr.RawGetInt(2).(*lua.LTable)
	if int(lua.LVAsNumber(second.RawGetString("part_number"))) != 7 {
		t.Error("part_number mismatch")
	}
	if second.RawGetString("url").String() != "https://p7" {
		t.Error("url mismatch")
	}
}

func TestMultipartYields_UnsupportedError(t *testing.T) {
	l := lua.NewState()
	defer l.Close()

	y := AcquireCreateMultipartUploadYield()
	defer ReleaseCreateMultipartUploadYield(y)

	res := y.HandleResult(l, csapi.CreateMultipartUploadResponse{
		Error: csapi.ErrMultipartUnsupported,
	}, nil)
	if res[0] != lua.LNil {
		t.Fatal("expected nil result on unsupported")
	}
	if res[1] == lua.LNil {
		t.Fatal("expected error value on unsupported")
	}
	if !strings.Contains(res[1].String(), "multipart") {
		t.Fatalf("expected multipart-unsupported message, got %q", res[1].String())
	}
}

func newStorageCallState(t *testing.T) (*lua.LState, *lua.LUserData) {
	t.Helper()
	l := lua.NewState()
	t.Cleanup(l.Close)
	ud := l.NewUserData()
	ud.Value = &storageWrapper{}
	return l, ud
}

func TestStoragePresignedPartURLs_ValidatesAndPreservesHeaders(t *testing.T) {
	t.Run("requires exactly one selector", func(t *testing.T) {
		l, storage := newStorageCallState(t)
		opts := l.CreateTable(0, 2)
		opts.RawSetString("parts", l.CreateTable(0, 0))
		opts.RawSetString("count", lua.LNumber(1))
		l.Push(storage)
		l.Push(lua.LString("key"))
		l.Push(lua.LString("upload"))
		l.Push(opts)

		assertInvalidLuaResult(t, l, storagePresignedPartURLs(l))
	})

	t.Run("rejects fractional and duplicate parts", func(t *testing.T) {
		for _, parts := range [][]lua.LValue{
			{lua.LNumber(1.5)},
			{lua.LNumber(1), lua.LNumber(1)},
		} {
			l, storage := newStorageCallState(t)
			partTable := l.CreateTable(len(parts), 0)
			for i, part := range parts {
				partTable.RawSetInt(i+1, part)
			}
			opts := l.CreateTable(0, 1)
			opts.RawSetString("parts", partTable)
			l.Push(storage)
			l.Push(lua.LString("key"))
			l.Push(lua.LString("upload"))
			l.Push(opts)

			assertInvalidLuaResult(t, l, storagePresignedPartURLs(l))
		}
	})

	t.Run("preserves string headers", func(t *testing.T) {
		l, storage := newStorageCallState(t)
		headers := l.CreateTable(0, 1)
		headers.RawSetString("x-amz-request-payer", lua.LString("requester"))
		opts := l.CreateTable(0, 2)
		opts.RawSetString("count", lua.LNumber(1))
		opts.RawSetString("headers", headers)
		l.Push(storage)
		l.Push(lua.LString("key"))
		l.Push(lua.LString("upload"))
		l.Push(opts)

		if got := storagePresignedPartURLs(l); got != -1 {
			t.Fatalf("expected yield result, got %d", got)
		}
		yield, ok := l.Get(-1).(*PresignedPartURLsYield)
		if !ok {
			t.Fatalf("expected PresignedPartURLsYield, got %T", l.Get(-1))
		}
		defer ReleasePresignedPartURLsYield(yield)
		if got := yield.Options.Headers["x-amz-request-payer"]; got != "requester" {
			t.Fatalf("header = %q, want requester", got)
		}
	})

	t.Run("rejects non-table headers", func(t *testing.T) {
		l, storage := newStorageCallState(t)
		opts := l.CreateTable(0, 2)
		opts.RawSetString("count", lua.LNumber(1))
		opts.RawSetString("headers", lua.LString("invalid"))
		l.Push(storage)
		l.Push(lua.LString("key"))
		l.Push(lua.LString("upload"))
		l.Push(opts)

		assertInvalidLuaResult(t, l, storagePresignedPartURLs(l))
	})
}

func TestStorageCompleteMultipartUpload_RejectsMissingETag(t *testing.T) {
	l, storage := newStorageCallState(t)
	part := l.CreateTable(0, 1)
	part.RawSetString("part_number", lua.LNumber(1))
	parts := l.CreateTable(1, 0)
	parts.RawSetInt(1, part)
	l.Push(storage)
	l.Push(lua.LString("key"))
	l.Push(lua.LString("upload"))
	l.Push(parts)

	assertInvalidLuaResult(t, l, storageCompleteMultipartUpload(l))
}

func TestStorageOpenReader_RejectsOversizedCache(t *testing.T) {
	l, storage := newStorageCallState(t)
	opts := l.CreateTable(0, 2)
	opts.RawSetString("block_size", lua.LNumber(csapi.MaxReaderBlockSize))
	opts.RawSetString("cache_blocks", lua.LNumber(3))
	l.Push(storage)
	l.Push(lua.LString("key"))
	l.Push(opts)

	assertInvalidLuaResult(t, l, storageOpenReader(l))
}

func TestBoundedInteger_AcceptsIntegerSubtype(t *testing.T) {
	cases := []struct {
		value lua.LValue
		name  string
		want  int64
		valid bool
	}{
		{lua.LInteger(1), "integer at lower bound", 1, true},
		{lua.LInteger(csapi.MaxPartNumber), "integer at upper bound", csapi.MaxPartNumber, true},
		{lua.LInteger(0), "integer below range", 0, false},
		{lua.LInteger(csapi.MaxPartNumber + 1), "integer above range", 0, false},
		{lua.LNumber(3), "integral float", 3, true},
		{lua.LNumber(1.5), "fractional float", 0, false},
		{lua.LString("3"), "numeric string", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, valid := boundedInteger(tc.value, 1, csapi.MaxPartNumber)
			if valid != tc.valid || (valid && got != tc.want) {
				t.Fatalf("boundedInteger(%v) = (%d, %t), want (%d, %t)", tc.value, got, valid, tc.want, tc.valid)
			}
		})
	}
}

// Numbers written in Lua source arrive as the lua.LInteger subtype, unlike
// tables assembled from Go with lua.LNumber values, so these calls must go
// through DoString to cover the path real scripts take.
func TestStorageCalls_AcceptLuaIntegerOptions(t *testing.T) {
	luaTable := func(t *testing.T, l *lua.LState, src string) lua.LValue {
		t.Helper()
		if err := l.DoString("opts = " + src); err != nil {
			t.Fatalf("build options from Lua: %v", err)
		}
		return l.GetGlobal("opts")
	}

	t.Run("presigned part urls via parts", func(t *testing.T) {
		l, storage := newStorageCallState(t)
		opts := luaTable(t, l, `{ parts = {1, 2, 3} }`)
		l.Push(storage)
		l.Push(lua.LString("key"))
		l.Push(lua.LString("upload"))
		l.Push(opts)

		if got := storagePresignedPartURLs(l); got != -1 {
			t.Fatalf("expected yield result, got %d (%v)", got, l.Get(-1))
		}
		yield, ok := l.Get(-1).(*PresignedPartURLsYield)
		if !ok {
			t.Fatalf("expected PresignedPartURLsYield, got %T", l.Get(-1))
		}
		defer ReleasePresignedPartURLsYield(yield)
		want := []int32{1, 2, 3}
		if len(yield.Options.PartNumbers) != len(want) {
			t.Fatalf("part numbers = %v, want %v", yield.Options.PartNumbers, want)
		}
		for i, n := range want {
			if yield.Options.PartNumbers[i] != n {
				t.Fatalf("part numbers = %v, want %v", yield.Options.PartNumbers, want)
			}
		}
	})

	t.Run("presigned part urls via count and expiration", func(t *testing.T) {
		l, storage := newStorageCallState(t)
		opts := luaTable(t, l, `{ count = 3, expiration = 900 }`)
		l.Push(storage)
		l.Push(lua.LString("key"))
		l.Push(lua.LString("upload"))
		l.Push(opts)

		if got := storagePresignedPartURLs(l); got != -1 {
			t.Fatalf("expected yield result, got %d (%v)", got, l.Get(-1))
		}
		yield, ok := l.Get(-1).(*PresignedPartURLsYield)
		if !ok {
			t.Fatalf("expected PresignedPartURLsYield, got %T", l.Get(-1))
		}
		defer ReleasePresignedPartURLsYield(yield)
		if len(yield.Options.PartNumbers) != 3 {
			t.Fatalf("part numbers = %v, want 1..3", yield.Options.PartNumbers)
		}
		if yield.Expiration != 900 {
			t.Fatalf("expiration = %d, want 900", yield.Expiration)
		}
	})

	t.Run("complete multipart upload", func(t *testing.T) {
		l, storage := newStorageCallState(t)
		parts := luaTable(t, l, `{ { part_number = 1, etag = "etag-1" }, { part_number = 2, etag = "etag-2" } }`)
		l.Push(storage)
		l.Push(lua.LString("key"))
		l.Push(lua.LString("upload"))
		l.Push(parts)

		if got := storageCompleteMultipartUpload(l); got != -1 {
			t.Fatalf("expected yield result, got %d (%v)", got, l.Get(-1))
		}
		yield, ok := l.Get(-1).(*CompleteMultipartUploadYield)
		if !ok {
			t.Fatalf("expected CompleteMultipartUploadYield, got %T", l.Get(-1))
		}
		defer ReleaseCompleteMultipartUploadYield(yield)
		if len(yield.Parts) != 2 ||
			yield.Parts[0].PartNumber != 1 || yield.Parts[0].ETag != "etag-1" ||
			yield.Parts[1].PartNumber != 2 || yield.Parts[1].ETag != "etag-2" {
			t.Fatalf("parts = %+v, want part 1/etag-1 and part 2/etag-2", yield.Parts)
		}
	})

	t.Run("open reader options", func(t *testing.T) {
		l, storage := newStorageCallState(t)
		opts := luaTable(t, l, `{ block_size = 65536, cache_blocks = 2 }`)
		l.Push(storage)
		l.Push(lua.LString("key"))
		l.Push(opts)

		if got := storageOpenReader(l); got != -1 {
			t.Fatalf("expected yield result, got %d (%v)", got, l.Get(-1))
		}
		yield, ok := l.Get(-1).(*OpenReaderYield)
		if !ok {
			t.Fatalf("expected OpenReaderYield, got %T", l.Get(-1))
		}
		defer ReleaseOpenReaderYield(yield)
		if yield.BlockSize != 65536 || yield.CacheBlocks != 2 {
			t.Fatalf("block_size = %d, cache_blocks = %d, want 65536 and 2", yield.BlockSize, yield.CacheBlocks)
		}
	})
}

func assertInvalidLuaResult(t *testing.T, l *lua.LState, returns int) {
	t.Helper()
	if returns != 2 {
		t.Fatalf("return count = %d, want 2", returns)
	}
	if l.Get(-2) != lua.LNil {
		t.Fatalf("first result = %v, want nil", l.Get(-2))
	}
	luaErr, ok := l.Get(-1).(*lua.Error)
	if !ok {
		t.Fatalf("error result = %T, want *lua.Error", l.Get(-1))
	}
	if luaErr.Kind() != lua.Invalid {
		t.Fatalf("error kind = %s, want %s", luaErr.Kind(), lua.Invalid)
	}
}
