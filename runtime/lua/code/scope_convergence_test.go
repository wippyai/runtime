package code_test

import (
	"os"
	"testing"

	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/code"
	"github.com/wippyai/runtime/runtime/lua/modules/funcs"
	iomod "github.com/wippyai/runtime/runtime/lua/modules/io"
	"github.com/wippyai/runtime/runtime/lua/modules/registry"
	"github.com/wippyai/runtime/runtime/lua/modules/time"
	"github.com/yuin/gopher-lua/types/diag"
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
