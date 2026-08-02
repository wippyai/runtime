// SPDX-License-Identifier: MPL-2.0

package registry

import (
	"errors"
	"testing"

	lua "github.com/wippyai/go-lua"
	"go.uber.org/zap"
)

func TestNewLoaderModule(t *testing.T) {
	log := zap.NewNop()
	module := NewLoaderModule(LoaderOptions{Log: log})

	if module == nil {
		t.Fatal("expected module to be non-nil")
		return
	}

	if module.Name != loaderModuleName {
		t.Errorf("expected name %s, got %s", loaderModuleName, module.Name)
	}
}

func TestNewLoaderModuleWithDefaultOptions(t *testing.T) {
	module := NewLoaderModule(DefaultLoaderOptions())

	if module == nil {
		t.Fatal("expected module to be non-nil")
		return
	}

	if module.Name != loaderModuleName {
		t.Errorf("expected name %s, got %s", loaderModuleName, module.Name)
	}
}

func TestLoaderModuleInfo(t *testing.T) {
	module := NewLoaderModule(DefaultLoaderOptions())

	if module.Name != loaderModuleName {
		t.Errorf("expected name %s, got %s", loaderModuleName, module.Name)
	}

	if module.Description == "" {
		t.Error("expected non-empty description")
	}

	if len(module.Class) == 0 {
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
