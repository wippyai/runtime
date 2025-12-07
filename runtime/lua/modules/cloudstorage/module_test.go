package cloudstorage

import (
	"testing"

	csapi "github.com/wippyai/runtime/api/cloudstorage"
	lua "github.com/yuin/gopher-lua"
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
		int(csapi.CmdListObjects):     false,
		int(csapi.CmdDownloadObject):  false,
		int(csapi.CmdUploadObject):    false,
		int(csapi.CmdDeleteObjects):   false,
		int(csapi.CmdPresignedGetURL): false,
		int(csapi.CmdPresignedPutURL): false,
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
