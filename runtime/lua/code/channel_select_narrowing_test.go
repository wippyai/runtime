package code_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/wippyai/go-lua/types/diag"
	"github.com/wippyai/go-lua/types/io"
	api "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/code"
	"github.com/wippyai/runtime/runtime/lua/engine"
	processmod "github.com/wippyai/runtime/runtime/lua/modules/process"
	timemod "github.com/wippyai/runtime/runtime/lua/modules/time"
)

func TestChannelSelectNarrowing_ProcessEvent(t *testing.T) {
	source := `
local time = require("time")

local function main()
    local events_ch = process.events()
    local timeout = time.after("3s")
    local result = channel.select {
        events_ch:case_receive(),
        timeout:case_receive(),
    }

    if result.channel == timeout then
        return false, "timeout"
    end

    local event = result.value
    local result_value = event.result
    local msg = result_value.value
    return msg
end

return { main = main }
`

	tc := code.NewTypeChecker(code.TypeCheckConfig{Enabled: true, Strict: true}, []*api.ModuleDef{
		engine.ChannelModule,
		processmod.Module,
		timemod.Module,
	})

	_, diags, err := tc.Check(source, "test.lua", nil)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	for _, d := range diags {
		if d.Severity == diag.SeverityError {
			t.Fatalf("unexpected error: %s", d.Message)
		}
	}
}

func TestChannelSelectNarrowing_TrapLinksEnabled_Source(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	root := filepath.Clean(filepath.Join(wd, "..", "..", "..", ".."))
	assertPath := filepath.Join(root, "tests", "app", "src", "lib", "assert.lua")
	testPath := filepath.Join(root, "tests", "app", "src", "test", "process", "trap_links_enabled.lua")
	if _, err := os.Stat(assertPath); err != nil {
		root = filepath.Clean(filepath.Join(wd, "..", "..", ".."))
		assertPath = filepath.Join(root, "tests", "app", "src", "lib", "assert.lua")
		testPath = filepath.Join(root, "tests", "app", "src", "test", "process", "trap_links_enabled.lua")
	}

	assertSource, err := os.ReadFile(assertPath)
	if err != nil {
		t.Fatalf("read assert source: %v", err)
	}
	testSource, err := os.ReadFile(testPath)
	if err != nil {
		t.Fatalf("read test source: %v", err)
	}

	tc := code.NewTypeChecker(code.TypeCheckConfig{Enabled: true, Strict: true}, []*api.ModuleDef{
		engine.ChannelModule,
		processmod.Module,
		timemod.Module,
	})

	assertManifest, assertDiags, err := tc.Check(string(assertSource), "assert2.lua", nil)
	if err != nil {
		t.Fatalf("assert2 parse error: %v", err)
	}
	for _, d := range assertDiags {
		if d.Severity == diag.SeverityError {
			t.Fatalf("assert2 error: %s", d.Message)
		}
	}

	imports := map[string]*io.Manifest{
		"assert2": assertManifest,
	}
	_, diags, err := tc.Check(string(testSource), "trap_links_enabled.lua", imports)
	if err != nil {
		t.Fatalf("trap_links_enabled parse error: %v", err)
	}
	for _, d := range diags {
		if d.Severity == diag.SeverityError {
			t.Fatalf("unexpected error at line %d: %s", d.Position.Line, d.Message)
		}
	}
}
