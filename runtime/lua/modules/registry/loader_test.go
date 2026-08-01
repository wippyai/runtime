// SPDX-License-Identifier: MPL-2.0

package registry

import (
	"context"
	"errors"
	"testing"

	lua "github.com/wippyai/go-lua"
	moduleapi "github.com/wippyai/runtime/api/modules"
	regapi "github.com/wippyai/runtime/api/registry"
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

func TestLoadSourcesUsesRegisteredCapability(t *testing.T) {
	ctx := setupContextWithTranscoder()
	moduleapi.WithSourceLoader(ctx, func(context.Context) ([]regapi.Entry, error) {
		return []regapi.Entry{{ID: regapi.NewID("example.source", "probe"), Kind: "registry.entry"}}, nil
	})
	l := lua.NewState()
	defer l.Close()
	l.SetContext(ctx)

	module := NewLoaderModule(DefaultLoaderOptions())
	table, _ := module.Build()
	l.SetGlobal("loader", table)
	if err := l.DoString(`
		local entries, err = loader.load_sources()
		assert(err == nil, tostring(err))
		assert(#entries == 1)
		assert(entries[1].id == "example.source:probe")
	`); err != nil {
		t.Fatal(err)
	}
}

func TestLoadSourcesRejectsMissingCapability(t *testing.T) {
	ctx := setupContextWithTranscoder()
	l := lua.NewState()
	defer l.Close()
	l.SetContext(ctx)

	module := NewLoaderModule(DefaultLoaderOptions())
	table, _ := module.Build()
	l.SetGlobal("loader", table)
	if err := l.DoString(`
		local entries, err = loader.load_sources()
		assert(entries == nil)
		assert(err ~= nil)
	`); err != nil {
		t.Fatal(err)
	}
}

func TestLoadSourcesRequiresPermission(t *testing.T) {
	ctx := setupStrictContextWithTranscoder()
	l := lua.NewState()
	defer l.Close()
	l.SetContext(ctx)

	module := NewLoaderModule(DefaultLoaderOptions())
	table, _ := module.Build()
	l.SetGlobal("loader", table)
	if err := l.DoString(`
		local entries, err = loader.load_sources()
		assert(entries == nil)
		assert(err ~= nil)
	`); err != nil {
		t.Fatal(err)
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
