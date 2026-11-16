// SPDX-License-Identifier: MPL-2.0

package xml

import (
	"context"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/dispatcher"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/api/runtime/resource"
	streammod "github.com/wippyai/runtime/runtime/lua/modules/stream"
	streamsys "github.com/wippyai/runtime/system/stream"
	"github.com/wippyai/runtime/system/xml/xmlcmd"
)

// prelude defines assertion helpers and a canonical element dump for test scripts.
const prelude = `
function eq(a, b, msg)
	if a ~= b then error((msg or "eq") .. ": expected " .. tostring(b) .. ", got " .. tostring(a), 2) end
end
function check(v, msg)
	if not v then error(msg or "check failed", 2) end
	return v
end
function code(err)
	if err == nil then return nil end
	local d = err:details()
	return d and d.code
end
local function quote(s) return '"' .. s:gsub('\\', '\\\\'):gsub('"', '\\"'):gsub('\n', '\\n'):gsub('\r', '\\r'):gsub('\t', '\\t') .. '"' end
local function map(open, close, m)
	if m == nil then return "" end
	local keys = {}
	for k in pairs(m) do keys[#keys + 1] = k end
	table.sort(keys)
	local parts = {}
	for _, k in ipairs(keys) do parts[#parts + 1] = k .. "=" .. quote(m[k]) end
	return open .. table.concat(parts, " ") .. close
end
function dump(e)
	local s = e.name
	if e.ns then s = s .. "{" .. e.ns .. "}" end
	s = s .. map("[", "]", e.attrs) .. map("(", ")", e.xmlns)
	if e.text then s = s .. quote(e.text) end
	if e.children then
		local parts = {}
		for _, c in ipairs(e.children) do
			if type(c) == "string" then parts[#parts + 1] = quote(c) else parts[#parts + 1] = dump(c) end
		end
		s = s .. "<" .. table.concat(parts, ",") .. ">"
	end
	return s
end
`

type result struct {
	data any
	err  error
}

type receiver chan result

func (r receiver) CompleteYield(_ uint64, data any, err error) { r <- result{data, err} }

// env runs Lua scripts against the module, executing yields on an xml dispatcher.
type env struct {
	t        testing.TB
	l        *lua.LState
	ctx      context.Context
	cmdCtx   context.Context
	table    *resource.Table
	store    *resource.Store
	handlers map[dispatcher.CommandID]dispatcher.Handler
	yields   map[dispatcher.CommandID]int
}

func newEnv(t testing.TB) *env {
	t.Helper()
	l := lua.NewState()
	lua.OpenErrors(l)
	mod, _ := Module.Build()
	l.SetGlobal("xml", mod)
	ctx, fc := ctxapi.OpenFrameContext(context.Background())
	store := resource.NewStore()
	require.NoError(t, resource.SetStore(ctx, store))
	l.SetContext(ctx)
	e := &env{t: t, l: l, ctx: ctx, cmdCtx: ctx, table: store.Table(), store: store,
		handlers: map[dispatcher.CommandID]dispatcher.Handler{}, yields: map[dispatcher.CommandID]int{}}
	register := func(id dispatcher.CommandID, h dispatcher.Handler) { e.handlers[id] = h }
	xmlcmd.NewDispatcher().RegisterAll(register)
	streamsys.NewDispatcher().RegisterAll(register)
	require.NoError(t, l.DoString(prelude))
	t.Cleanup(func() {
		l.Close()
		_ = store.Close()
		ctxapi.ReleaseFrameContext(fc)
	})
	return e
}

// newBareEnv runs scripts without a context, as in states that hold no resources.
func newBareEnv(t testing.TB) *env {
	t.Helper()
	l := lua.NewState()
	lua.OpenErrors(l)
	mod, _ := Module.Build()
	l.SetGlobal("xml", mod)
	require.NoError(t, l.DoString(prelude))
	t.Cleanup(l.Close)
	return &env{t: t, l: l, yields: map[dispatcher.CommandID]int{}}
}

func (e *env) exec(script string) ([]lua.LValue, error) {
	fn, err := e.l.LoadString(script)
	if err != nil {
		return nil, err
	}
	co := e.l.NewThreadWithContext(e.ctx)
	st, vals, err := e.l.Resume(co, fn)
	for err == nil && st == lua.ResumeYield {
		y := vals[len(vals)-1]
		conv, ok := y.(luaapi.YieldConverter)
		require.True(e.t, ok, "unexpected yield %v", y)
		cmd := conv.ToCommand()
		e.yields[cmd.CmdID()]++
		h := e.handlers[cmd.CmdID()]
		require.NotNil(e.t, h, "no handler for %d", cmd.CmdID())
		r := make(receiver, 1)
		require.NoError(e.t, h.Handle(e.cmdCtx, cmd, 1, r))
		res := <-r
		back := y.(luaapi.HandledYield).HandleResult(e.l, res.data, res.err)
		st, vals, err = e.l.Resume(co, nil, back...)
	}
	return vals, err
}

// run executes a script that must succeed and returns its results.
func (e *env) run(script string) []lua.LValue {
	e.t.Helper()
	vals, err := e.exec(script)
	require.NoError(e.t, err)
	return vals
}

func (e *env) yieldCount() int {
	n := 0
	for _, c := range e.yields {
		n += c
	}
	return n
}

// global sets a Lua global for the next scripts.
func (e *env) global(name string, v lua.LValue) { e.l.SetGlobal(name, v) }

// stream inserts r as a readable stream and returns its Lua handle.
func (e *env) stream(r io.Reader) lua.LValue {
	return streammod.NewStream(e.l, streamsys.Insert(e.table, io.NopCloser(r)))
}

type sinkStream struct {
	io.Writer
	closed bool
}

func (s *sinkStream) Close() error { s.closed = true; return nil }

// sink inserts w as a writable stream and returns its Lua handle.
func (e *env) sink(w io.Writer) (lua.LValue, *sinkStream) {
	s := &sinkStream{Writer: w}
	return streammod.NewStream(e.l, streamsys.Insert(e.table, s)), s
}
