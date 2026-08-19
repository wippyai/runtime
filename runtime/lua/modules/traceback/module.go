// SPDX-License-Identifier: MPL-2.0

// Package traceback provides Python-style stack traces with local variables
// and upvalues for the Wippy Lua runtime. It is the convenience layer over
// the read-only debug subset: debug.traceback/getinfo/getlocal/getupvalue are
// the primitives; traceback.format/frames give you "frames + locals in one
// call", which Lua's standard debug.traceback does not provide.
package traceback

import (
	"fmt"
	"strings"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/go-lua/inspect"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
)

// Module is the traceback module definition, registered as a core ambient
// module so it is both require-able (require("traceback")) and available as a
// global.
var Module = &luaapi.ModuleDef{
	Name:        "traceback",
	Description: "Python-style stack traces with local variables and upvalues",
	Class:       []string{luaapi.ClassIO},
	Build:       buildModule,
}

func buildModule() (*lua.LTable, []luaapi.YieldType) {
	tbl := lua.CreateTable(0, 2)
	tbl.RawSetString("format", lua.LGoFunc(formatFunc))
	tbl.RawSetString("frames", lua.LGoFunc(framesFunc))
	tbl.Immutable = true

	return tbl, nil
}

// framesFunc implements traceback.frames([level[, opts]]). It returns a
// 1-indexed array of frame tables, one per stack level from `level` up to the
// top of the stack (capped by opts.depth, default 20). Each frame is
// { source, currentline, name, what, locals, upvalues } where locals and
// upvalues are arrays of { name, value } pairs.
func framesFunc(l *lua.LState) int {
	level := optLevel(l, 1)
	opts := parseOptions(l, 2)

	result := l.NewTable()
	count := 0
	for lvl := level; count < opts.Depth && lvl < level+opts.Depth+maxSkippedFrames; lvl++ {
		frame, status := captureFrame(l, lvl, opts)
		if status == frameEnd {
			break
		}

		if status == frameSkip {
			continue
		}

		count++
		result.RawSetInt(count, frame)
	}

	l.Push(result)

	return 1
}

// formatFunc implements traceback.format([level[, opts]]). It returns a
// formatted multi-line string (Python traceback.format_exc style) covering
// frames from `level` up to the top (capped by opts.depth).
func formatFunc(l *lua.LState) int {
	level := optLevel(l, 1)
	opts := parseOptions(l, 2)

	var sb strings.Builder
	seen := make(map[*lua.LTable]bool)
	count := 0
	for lvl := level; count < opts.Depth && lvl < level+opts.Depth+maxSkippedFrames; lvl++ {
		fr, status := safeGetFrame(l, lvl)
		if status == frameEnd {
			break
		}

		if status == frameSkip {
			continue
		}

		count++
		writeFrame(&sb, fr, opts, seen)
	}

	l.Push(lua.LString(sb.String()))

	return 1
}

const maxSkippedFrames = 64

type frameStatus int

const (
	frameCaptured frameStatus = iota
	frameSkip                 // introspection panicked for this frame (e.g. a Go frame whose
	// internals are stale right after an error recover); skip it
	frameEnd // no frame at this level (end of stack)
)

func optLevel(l *lua.LState, idx int) int {
	if l.GetTop() < idx {
		return 1
	}

	v := l.Get(idx)
	if !isNumber(v) {
		return 1
	}

	return int(lua.LVAsNumber(v))
}

func isNumber(v lua.LValue) bool {
	return v.Type() == lua.LTNumber || v.Type() == lua.LTInteger
}

// safeGetFrame wraps inspect.GetStackFrame with a recover. Right after an
// error recover (inside an xpcall handler, for example) some intermediate Go
// frames can have stale internals that make GetStackFrame panic; rather than
// abort the whole trace, we skip those frames so the throw site and other
// valid frames are still captured.
func safeGetFrame(l *lua.LState, level int) (fr inspect.StackFrame, status frameStatus) {
	defer func() {
		if r := recover(); r != nil {
			status = frameSkip
		}
	}()

	var ok bool
	fr, ok = inspect.GetStackFrame(l, level)
	if !ok {
		return inspect.StackFrame{}, frameEnd
	}

	return fr, frameCaptured
}

func captureFrame(l *lua.LState, level int, opts options) (lua.LValue, frameStatus) {
	fr, status := safeGetFrame(l, level)
	if status != frameCaptured {
		return lua.LNil, status
	}

	tbl := l.NewTable()
	tbl.RawSetString("source", lua.LString(fr.Source))
	tbl.RawSetString("currentline", lua.LNumber(fr.CurrentLine))
	tbl.RawSetString("name", lua.LString(fr.Name))
	tbl.RawSetString("what", lua.LString(fr.FuncType))

	if opts.Locals {
		localsTbl := l.NewTable()
		for i, p := range fr.Locals {
			addPair(l, localsTbl, i, p.Name, p.Value)
		}

		tbl.RawSetString("locals", localsTbl)
	}

	if opts.Upvalues {
		upTbl := l.NewTable()
		for i, p := range fr.Upvalues {
			addPair(l, upTbl, i, p.Name, p.Value)
		}

		tbl.RawSetString("upvalues", upTbl)
	}

	return tbl, frameCaptured
}

func addPair(l *lua.LState, tbl *lua.LTable, idx int, name string, value lua.LValue) {
	entry := l.NewTable()
	entry.RawSetString("name", lua.LString(name))
	entry.RawSetString("value", value)
	tbl.RawSetInt(idx+1, entry)
}

func writeFrame(sb *strings.Builder, fr inspect.StackFrame, opts options, seen map[*lua.LTable]bool) {
	if sb.Len() > 0 {
		sb.WriteByte('\n')
	}

	name := fr.Name
	if name == "" {
		name = "?"
	}

	fmt.Fprintf(sb, "[%d] %s:%d (%s)", fr.Level, fr.Source, fr.CurrentLine, name)

	if opts.Locals && len(fr.Locals) > 0 {
		sb.WriteString("\n    Locals:")
		for _, p := range fr.Locals {
			sb.WriteString("\n      ")
			sb.WriteString(p.Name)
			sb.WriteString(" = ")
			renderValue(sb, p.Value, 0, opts, seen)
		}
	}

	if opts.Upvalues && len(fr.Upvalues) > 0 {
		sb.WriteString("\n    Upvalues:")
		for _, p := range fr.Upvalues {
			sb.WriteString("\n      ")
			sb.WriteString(p.Name)
			sb.WriteString(" = ")
			renderValue(sb, p.Value, 0, opts, seen)
		}
	}
}

// renderValue writes a Lua value's printable form. Tables are rendered with
// depth limiting and cycle detection so a self-referential local (the common
// case when debugging stateful code) cannot make format() loop forever.
func renderValue(sb *strings.Builder, v lua.LValue, depth int, opts options, seen map[*lua.LTable]bool) {
	switch lv := v.(type) {
	case *lua.LNilType:
		sb.WriteString("nil")
	case lua.LBool:
		if bool(lv) {
			sb.WriteString("true")
		} else {
			sb.WriteString("false")
		}
	case lua.LNumber, lua.LInteger:
		sb.WriteString(v.String())
	case lua.LString:
		writeTruncated(sb, string(lv), opts.MaxValueLen)
	case *lua.LTable:
		renderTable(sb, lv, depth, opts, seen)
	case *lua.LFunction:
		sb.WriteString("<function>")
	case *lua.LState:
		sb.WriteString("<thread>")
	case *lua.LUserData:
		sb.WriteString("<userdata>")
	default:
		writeTruncated(sb, v.String(), opts.MaxValueLen)
	}
}

func renderTable(sb *strings.Builder, t *lua.LTable, depth int, opts options, seen map[*lua.LTable]bool) {
	if depth >= opts.MaxTableDepth {
		sb.WriteString("{...}")

		return
	}

	if seen[t] {
		sb.WriteString("{cyclic}")

		return
	}

	seen[t] = true
	deleteOnExit := depth == 0
	defer func() {
		if deleteOnExit {
			delete(seen, t)
		}
	}()

	var parts []string
	t.ForEach(func(k, val lua.LValue) {
		var ks strings.Builder
		renderValue(&ks, k, depth+1, opts, seen)

		var vs strings.Builder
		renderValue(&vs, val, depth+1, opts, seen)

		parts = append(parts, ks.String()+" = "+vs.String())
	})

	if len(parts) == 0 {
		sb.WriteString("{}")

		return
	}

	sb.WriteByte('{')
	sb.WriteString(strings.Join(parts, ", "))
	sb.WriteByte('}')
}

func writeTruncated(sb *strings.Builder, s string, maxLen int) {
	if maxLen > 0 && len(s) > maxLen {
		sb.WriteString(s[:maxLen])
		sb.WriteString("...")

		return
	}

	sb.WriteString(s)
}
