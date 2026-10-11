// SPDX-License-Identifier: MPL-2.0

package engine

import (
	"context"
	"testing"

	lua "github.com/wippyai/go-lua"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/process"
)

// Protected failures must unwind the callee without closing the surviving
// actor's captured locals, including across an external yield.
func TestProcessProtectedCallPreservesCallerUpvalues(t *testing.T) {
	for _, tc := range []struct {
		name  string
		call  string
		yield bool
	}{
		{"pcall", `local ok = pcall(error, "expected"); assert(not ok)`, false},
		{"xpcall", `local ok = xpcall(function() error("expected") end, function(e) return e end); assert(not ok)`, false},
		{"typed error", `local ok, err = pcall(function() error(errors.new({message="expected", kind=errors.INVALID, retryable=false})) end); assert(not ok and err:kind() == errors.INVALID)`, false},
		{"after external yield", `local ok = pcall(function() test_yield(1); error("expected") end); assert(not ok)`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			factory := NewFactory(FactoryConfig{
				Script: `return {main=function()
					local value = 0
					local function set(x) value = x end
					` + tc.call + `
					set(42)
					assert(value == 42, "caught error detached the caller's captured local")
				end}`,
				ScriptName:    "protected_call.lua",
				ModuleBinders: []ModuleBinder{bindTestYield, wrapBinder(func(l *lua.LState) { lua.OpenErrors(l) })},
			})
			actor, err := factory()
			if err != nil {
				t.Fatal(err)
			}
			proc := actor.(*Process)
			defer proc.Close()
			ctx, _ := ctxapi.OpenFrameContext(context.Background())
			if err := proc.Init(ctx, "main", nil); err != nil {
				t.Fatal(err)
			}
			var output process.StepOutput
			if err := proc.Step(nil, &output); err != nil {
				t.Fatal(err)
			}
			if tc.yield {
				if len(output.Yields()) != 1 {
					t.Fatalf("yields = %v, want one external yield", output.Yields())
				}
				tag := output.Yields()[0].Tag
				output.Reset()
				if err := proc.Step([]process.Event{{Type: process.EventYieldComplete, Tag: tag}}, &output); err != nil {
					t.Fatal(err)
				}
			}
			if output.Status() != process.StepDone {
				t.Fatalf("status = %v, want StepDone", output.Status())
			}
		})
	}
}
