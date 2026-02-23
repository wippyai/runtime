// SPDX-License-Identifier: MPL-2.0

package cloudstorage

import (
	"errors"
	"strings"
	"testing"

	lua "github.com/wippyai/go-lua"
	csapi "github.com/wippyai/runtime/api/cloudstorage"
)

func TestModuleLoads(t *testing.T) {
	mod, yields := Module.Build()

	if mod == nil {
		t.Fatal("expected module table to be non-nil")
	}

	if len(yields) != 6 {
		t.Errorf("expected 6 yield types, got %d", len(yields))
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
						{Key: "test.txt", Size: 100, ContentType: "text/plain", ETag: "etag1"},
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
