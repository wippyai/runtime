package code_test

import (
	"os"
	"path/filepath"
	"testing"

	api "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/runtime/lua/code"
	"github.com/wippyai/runtime/runtime/lua/engine"
	processmod "github.com/wippyai/runtime/runtime/lua/modules/process"
	timemod "github.com/wippyai/runtime/runtime/lua/modules/time"
	"github.com/yuin/gopher-lua/compiler/ast"
	"github.com/yuin/gopher-lua/compiler/cfgbuild"
	"github.com/yuin/gopher-lua/compiler/check"
	"github.com/yuin/gopher-lua/compiler/parse"
	"github.com/yuin/gopher-lua/types/cfg"
	"github.com/yuin/gopher-lua/types/db"
	"github.com/yuin/gopher-lua/types/diag"
	"github.com/yuin/gopher-lua/types/flow"
	"github.com/yuin/gopher-lua/types/io"
	"github.com/yuin/gopher-lua/types/typ"
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
			stmts, parseErr := parse.ParseString(string(testSource), "trap_links_enabled.lua")
			if parseErr != nil {
				t.Fatalf("parse for debug: %v", parseErr)
			}

			ctx := db.NewContext(db.New())
			checker := check.New(ctx)
			checker.SetSourceName("trap_links_enabled.lua")

			base := tc.BuildEnv()
			if assertManifest.Export != nil {
				base = base.WithSymbol("assert2", assertManifest.Export)
			}
			checker.SetBaseScope(base)

			allImports := map[string]*io.Manifest{
				"channel": engine.ChannelModule.Types(),
				"process": processmod.Module.Types(),
				"time":    timemod.Module.Types(),
				"assert2": assertManifest,
			}
			checker.SetImports(allImports)

			_ = checker.Check(stmts)
			var fn *ast.FunctionExpr
			for _, stmt := range stmts {
				if localAssign, ok := stmt.(*ast.LocalAssignStmt); ok {
					for idx, name := range localAssign.Names {
						if name == "main" && idx < len(localAssign.Exprs) {
							if fnExpr, ok := localAssign.Exprs[idx].(*ast.FunctionExpr); ok {
								fn = fnExpr
								break
							}
						}
					}
				}
				if fn != nil {
					break
				}
				if fnStmt, ok := stmt.(*ast.FuncDefStmt); ok && fnStmt.Name != nil {
					if ident, ok := fnStmt.Name.Func.(*ast.IdentExpr); ok && ident.Value == "main" {
						fn = fnStmt.Func
						break
					}
				}
			}
			if fn == nil {
				t.Log("main function not found in AST")
				t.Fatalf("unexpected error: %s", d.Message)
			}

			g := cfgbuild.Build(fn)

			var eventPoint cfg.Point
			for _, p := range g.RPO() {
				node := g.Node(p)
				if node == nil {
					continue
				}
				if stmt, ok := node.AST.(*ast.LocalAssignStmt); ok {
					for _, name := range stmt.Names {
						if name == "event" {
							eventPoint = p
							break
						}
					}
				}
				if eventPoint != 0 {
					break
				}
			}
			if eventPoint != 0 {
				timeoutType := checker.NarrowedTypeAt(g, eventPoint, flow.Path{Root: "timeout"})
				resultType := checker.NarrowedTypeAt(g, eventPoint, flow.Path{Root: "result"})
				eventType := checker.NarrowedTypeAt(g, eventPoint, flow.Path{Root: "event"})
				resultValueType := checker.NarrowedTypeAt(g, eventPoint, flow.Path{
					Root: "result",
					Segments: []flow.PathSegment{
						{Kind: flow.PathField, Name: "value"},
					},
				})
				resultChannelType := checker.NarrowedTypeAt(g, eventPoint, flow.Path{
					Root: "result",
					Segments: []flow.PathSegment{
						{Kind: flow.PathField, Name: "channel"},
					},
				})
				declaredResultValue := checker.DeclaredTypeAt(ctx, g, eventPoint, flow.Path{
					Root: "result",
					Segments: []flow.PathSegment{
						{Kind: flow.PathField, Name: "value"},
					},
				})
				t.Logf("timeout type: %s", typeString(timeoutType))
				t.Logf("result type: %s", typeString(resultType))
				t.Logf("event type: %s", typeString(eventType))
				t.Logf("result.value type: %s", typeString(resultValueType))
				t.Logf("result.channel type: %s", typeString(resultChannelType))
				t.Logf("declared result.value type: %s", typeString(declaredResultValue))
			} else {
				t.Log("event assignment not found in CFG")
			}

			t.Fatalf("unexpected error: %s", d.Message)
		}
	}
}

func typeString(t typ.Type) string {
	if t == nil {
		return "<nil>"
	}
	return t.String()
}
