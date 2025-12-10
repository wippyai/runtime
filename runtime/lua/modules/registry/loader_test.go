package registry

import (
	"errors"
	"testing"

	lua "github.com/yuin/gopher-lua"
	"go.uber.org/zap"
)

func TestNewLoaderModule(t *testing.T) {
	log := zap.NewNop()
	module := NewLoaderModule(log)

	if module == nil {
		t.Fatal("expected module to be non-nil")
	}

	if module.log == nil {
		t.Error("expected log to be set")
	}
}

func TestNewLoaderModuleWithNilLogger(t *testing.T) {
	module := NewLoaderModule(nil)

	if module == nil {
		t.Fatal("expected module to be non-nil")
	}

	if module.log == nil {
		t.Error("expected default logger to be set")
	}
}

func TestLoaderModuleInfo(t *testing.T) {
	module := NewLoaderModule(nil)
	info := module.Info()

	if info.Name != loaderModuleName {
		t.Errorf("expected name %s, got %s", loaderModuleName, info.Name)
	}

	if info.Description == "" {
		t.Error("expected non-empty description")
	}

	if len(info.Class) == 0 {
		t.Error("expected at least one class")
	}
}

func TestTableToIDSuccess(t *testing.T) {
	l := newTestState()
	defer l.Close()

	tbl := l.CreateTable(0, 2)
	tbl.RawSetString("ns", lua.LString("test"))
	tbl.RawSetString("name", lua.LString("example"))

	id, err := tableToID(l, tbl)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if id.NS != "test" {
		t.Errorf("expected ns 'test', got %s", id.NS)
	}

	if id.Name != "example" {
		t.Errorf("expected name 'example', got %s", id.Name)
	}
}

func TestTableToIDMissingNS(t *testing.T) {
	l := newTestState()
	defer l.Close()

	tbl := l.CreateTable(0, 1)
	tbl.RawSetString("name", lua.LString("example"))

	_, err := tableToID(l, tbl)
	if !errors.Is(err, errIDFieldsRequired) {
		t.Errorf("expected errIDFieldsRequired, got %v", err)
	}
}

func TestTableToIDMissingName(t *testing.T) {
	l := newTestState()
	defer l.Close()

	tbl := l.CreateTable(0, 1)
	tbl.RawSetString("ns", lua.LString("test"))

	_, err := tableToID(l, tbl)
	if !errors.Is(err, errIDFieldsRequired) {
		t.Errorf("expected errIDFieldsRequired, got %v", err)
	}
}
