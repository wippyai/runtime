package code_test

import (
	"os"
	"testing"

	"github.com/wippyai/go-lua/types/diag"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/code"
	"github.com/wippyai/runtime/runtime/lua/engine"
	"github.com/wippyai/runtime/runtime/lua/modules/funcs"
	iomod "github.com/wippyai/runtime/runtime/lua/modules/io"
	"github.com/wippyai/runtime/runtime/lua/modules/process"
	"github.com/wippyai/runtime/runtime/lua/modules/registry"
	"github.com/wippyai/runtime/runtime/lua/modules/time"
)

func TestScopeConvergence_FullRunner(t *testing.T) {
	tc := code.NewTypeChecker(code.TypeCheckConfig{Enabled: true, Strict: true}, []*luaapi.ModuleDef{
		funcs.Module,
		time.Module,
		iomod.Module,
		registry.Module,
	})

	source, err := os.ReadFile("../../../tests/app/src/runner.lua")
	if err != nil {
		t.Fatalf("failed to read runner.lua: %v", err)
	}

	_, diags, err := tc.Check(string(source), "test.lua", nil)
	if err != nil {
		t.Fatalf("check error: %v", err)
	}
	for _, d := range diags {
		if d.Severity == diag.SeverityError {
			t.Logf("error: %s at %d:%d", d.Message, d.Position.Line, d.Position.Column)
		}
	}
}

func TestScopeConvergence_TrapLinksEnabled(t *testing.T) {
	tc := code.NewTypeChecker(code.TypeCheckConfig{Enabled: true, Strict: true}, []*luaapi.ModuleDef{
		engine.ChannelModule,
		process.Module,
		time.Module,
	})

	source, err := os.ReadFile("../../../tests/app/src/test/process/trap_links_enabled.lua")
	if err != nil {
		t.Fatalf("failed to read trap_links_enabled.lua: %v", err)
	}

	_, diags, err := tc.Check(string(source), "test.lua", nil)
	if err != nil {
		t.Fatalf("check error: %v", err)
	}

	t.Logf("total diagnostics: %d", len(diags))
	for _, d := range diags {
		t.Logf("[%s] %s at %d:%d", d.Code.Name(), d.Message, d.Position.Line, d.Position.Column)
	}

	hasResultError := false
	for _, d := range diags {
		if d.Severity == diag.SeverityError && d.Code.Name() == "E0004" {
			hasResultError = true
		}
	}
	if hasResultError {
		t.Error("got 'no field result' error - narrowing failed!")
	}
}
