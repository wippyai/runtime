package lua

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/dispatcher"
	gopherlua "github.com/yuin/gopher-lua"
)

func TestEventConstants(t *testing.T) {
	assert.Equal(t, "lua", System)
	assert.Equal(t, "lua.reset_code", InvalidateNodes)
}

func TestClassConstants(t *testing.T) {
	assert.Equal(t, "deterministic", ClassDeterministic)
	assert.Equal(t, "nondeterministic", ClassNondeterministic)
	assert.Equal(t, "io", ClassIO)
	assert.Equal(t, "network", ClassNetwork)
	assert.Equal(t, "encoding", ClassEncoding)
	assert.Equal(t, "time", ClassTime)
	assert.Equal(t, "process", ClassProcess)
	assert.Equal(t, "security", ClassSecurity)
	assert.Equal(t, "storage", ClassStorage)
	assert.Equal(t, "workflow", ClassWorkflow)
}

func TestModuleInfo(t *testing.T) {
	info := ModuleInfo{
		Name:        "testmod",
		Description: "Test module",
		Class:       []string{ClassDeterministic, ClassEncoding},
	}

	assert.Equal(t, "testmod", info.Name)
	assert.Equal(t, "Test module", info.Description)
	assert.Len(t, info.Class, 2)
}

func TestRegistration(t *testing.T) {
	reg := &Registration{
		Table: &gopherlua.LTable{},
		YieldTypes: []YieldType{
			{Sample: "test", CmdID: 1},
		},
	}

	assert.NotNil(t, reg.Table)
	assert.Len(t, reg.YieldTypes, 1)
}

func TestYieldType(t *testing.T) {
	yt := YieldType{
		Sample: "sample",
		CmdID:  42,
	}

	assert.Equal(t, "sample", yt.Sample)
	assert.Equal(t, dispatcher.CommandID(42), yt.CmdID)
}

func TestModuleDef_Info(t *testing.T) {
	mod := &ModuleDef{
		Name:        "mymod",
		Description: "My module",
		Class:       []string{ClassIO},
	}

	info := mod.Info()
	assert.Equal(t, "mymod", info.Name)
	assert.Equal(t, "My module", info.Description)
	assert.Equal(t, []string{ClassIO}, info.Class)
}

func TestModuleDef_Register_WithBuild(t *testing.T) {
	tbl := &gopherlua.LTable{}
	yields := []YieldType{{Sample: "test", CmdID: 1}}

	mod := &ModuleDef{
		Name:        "buildmod",
		Description: "Build module",
		Build: func() (*gopherlua.LTable, []YieldType) {
			return tbl, yields
		},
	}

	l := gopherlua.NewState()
	defer l.Close()

	reg := mod.Register(l)
	assert.NotNil(t, reg)
	assert.Equal(t, tbl, reg.Table)
	assert.Len(t, reg.YieldTypes, 1)
}

func TestModuleDef_Register_WithBuildValue(t *testing.T) {
	tbl := &gopherlua.LTable{}
	yields := []YieldType{{Sample: "test", CmdID: 2}}

	mod := &ModuleDef{
		Name:        "valuemod",
		Description: "Value module",
		BuildValue: func() (gopherlua.LValue, []YieldType) {
			return tbl, yields
		},
	}

	l := gopherlua.NewState()
	defer l.Close()

	reg := mod.Register(l)
	assert.NotNil(t, reg)
	assert.Equal(t, tbl, reg.Table)
	assert.Len(t, reg.YieldTypes, 1)
}

func TestModuleDef_Loader(t *testing.T) {
	tbl := &gopherlua.LTable{}
	mod := &ModuleDef{
		Name:        "loadermod",
		Description: "Loader module",
		Build: func() (*gopherlua.LTable, []YieldType) {
			return tbl, nil
		},
	}

	l := gopherlua.NewState()
	defer l.Close()

	count := mod.Loader(l)
	assert.Equal(t, 1, count)
}

func TestModuleDef_Load(t *testing.T) {
	tbl := &gopherlua.LTable{}
	yields := []YieldType{{Sample: "test", CmdID: 3}}

	mod := &ModuleDef{
		Name:        "loadmod",
		Description: "Load module",
		Build: func() (*gopherlua.LTable, []YieldType) {
			return tbl, yields
		},
	}

	l := gopherlua.NewState()
	defer l.Close()

	result := mod.Load(l)
	assert.Len(t, result, 1)

	global := l.GetGlobal("loadmod")
	assert.NotEqual(t, gopherlua.LNil, global)
}

func TestSetCodeManager_GetCodeManager(t *testing.T) {
	ctx := context.Background()
	appCtx := ctxapi.NewAppContext()
	ctx = ctxapi.WithAppContext(ctx, appCtx)

	cm := &mockCodeManager{modules: []ModuleInfo{{Name: "test"}}}
	ctx = SetCodeManager(ctx, cm)

	retrieved := GetCodeManager(ctx)
	assert.NotNil(t, retrieved)
	assert.Len(t, retrieved.GetModules(), 1)
}

func TestGetCodeManager_NoAppContext(t *testing.T) {
	ctx := context.Background()
	cm := GetCodeManager(ctx)
	assert.Nil(t, cm)
}

func TestGetCodeManager_NotSet(t *testing.T) {
	ctx := context.Background()
	appCtx := ctxapi.NewAppContext()
	ctx = ctxapi.WithAppContext(ctx, appCtx)

	cm := GetCodeManager(ctx)
	assert.Nil(t, cm)
}

func TestSetCodeManager_NoAppContext(t *testing.T) {
	ctx := context.Background()
	cm := &mockCodeManager{}
	result := SetCodeManager(ctx, cm)
	assert.Equal(t, ctx, result)
}

func TestSetCodeManager_SetOnce(t *testing.T) {
	ctx := context.Background()
	appCtx := ctxapi.NewAppContext()
	ctx = ctxapi.WithAppContext(ctx, appCtx)

	cm1 := &mockCodeManager{modules: []ModuleInfo{{Name: "first"}}}
	cm2 := &mockCodeManager{modules: []ModuleInfo{{Name: "second"}}}

	ctx = SetCodeManager(ctx, cm1)
	ctx = SetCodeManager(ctx, cm2)

	retrieved := GetCodeManager(ctx)
	assert.NotNil(t, retrieved)
	assert.Equal(t, "first", retrieved.GetModules()[0].Name)
}

func TestCodeManagerKey(t *testing.T) {
	assert.NotNil(t, CodeManagerKey)
	assert.Equal(t, "lua.codeManager", CodeManagerKey.Name)
}

type mockCodeManager struct {
	modules []ModuleInfo
}

func (m *mockCodeManager) GetModules() []ModuleInfo {
	return m.modules
}

// LoadModule moved to engine package - tests in runtime/lua/engine/core_modules_test.go
