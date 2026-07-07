// SPDX-License-Identifier: MPL-2.0

package traceback

import (
	"testing"

	lua "github.com/wippyai/go-lua"
)

// TestProof_Demo is the empirical end-to-end proof for the traceback feature.
// It wires the module exactly as the runtime does (the traceback module bound
// as a global, alongside the standard libs a real state gets) and exercises
// every capability: on-demand frames/format, locals + upvalues, depth/locals
// options, cyclic tables, and throw-site capture from inside an xpcall handler.
//
// The read-only debug-subset proof (BindCachedLibs stripping the mutating
// funcs) lives in engine/cached_libs_test.go, since that path requires the
// engine package.
//
// Run with `go test -v -run TestProof_Demo` and capture the output.
func TestProof_Demo(t *testing.T) {
	l := lua.NewState(lua.Options{SkipOpenLibs: true})
	defer l.Close()
	lua.OpenBase(l)
	lua.OpenString(l)
	lua.OpenTable(l)
	tbl, _ := Module.Build()
	l.SetGlobal(Module.Name, tbl)

	t.Run("on_demand_format_with_locals_and_upvalues", func(t *testing.T) {
		err := l.DoString(`
			local up = "i-am-an-upvalue"
			local function alpha(a, b)
				local sum = a + b
				print("=== traceback.format() (on-demand, like Python traceback.format_stack) ===")
				print(traceback.format(1))
				return sum
			end
			alpha(10, 32)
		`)
		if err != nil {
			t.Fatalf("format demo failed: %v", err)
		}
	})

	t.Run("on_demand_frames_structured", func(t *testing.T) {
		err := l.DoString(`
			local function beta()
				local flag = true
				local nested = { x = 1, y = { z = 2 } }
				print("=== traceback.frames() structured (locals + upvalues as Lua tables) ===")
				local frames = traceback.frames(1)
				for i = 1, #frames do
					local f = frames[i]
					local locStr = {}
					if f.locals then
						for j = 1, #f.locals do
							locStr[#locStr+1] = f.locals[j].name .. "=" .. tostring(f.locals[j].value)
						end
					end
					print(string.format("  frame[%d] %s:%d (%s) locals: %s",
						i, f.source, f.currentline, f.name, table.concat(locStr, ", ")))
				end
			end
			beta()
		`)
		if err != nil {
			t.Fatalf("frames demo failed: %v", err)
		}
	})

	t.Run("xpcall_captures_throw_site", func(t *testing.T) {
		err := l.DoString(`
			local function risky(depth)
				local context = "processing layer " .. depth
				local payload = { id = depth, secret = "shh" }
				if depth <= 0 then
					error("exploded at the bottom")
				end
				risky(depth - 1)
			end
			print("=== xpcall handler captures throw-site locals (Python traceback at exception) ===")
			local ok, err = xpcall(function() risky(3) end, function(e)
				print("caught:", tostring(e))
				print("--- traceback.format(1) from inside the handler: ---")
				print(traceback.format(1))
				return "recovered"
			end)
			print("xpcall returned:", ok, err)
		`)
		if err != nil {
			t.Fatalf("xpcall demo failed: %v", err)
		}
	})

	t.Run("cyclic_table_does_not_loop", func(t *testing.T) {
		err := l.DoString(`
			local function with_cycle()
				local t = { name = "root" }
				t.self = t
				t.child = { parent = t }
				print("=== cyclic table rendered without infinite recursion ===")
				print(traceback.format(1))
			end
			with_cycle()
		`)
		if err != nil {
			t.Fatalf("cyclic demo failed: %v", err)
		}
	})

	t.Run("opts_depth_and_locals_disabled", func(t *testing.T) {
		err := l.DoString(`
			local function f1() local a = 1
				local function f2() local b = 2
					local function f3() local c = 3
						print("=== format(1, {depth=2, locals=false}) — capped frames, no locals ===")
						print(traceback.format(1, { depth = 2, locals = false }))
					end
					f3()
				end
				f2()
			end
			f1()
		`)
		if err != nil {
			t.Fatalf("opts demo failed: %v", err)
		}
	})
}
