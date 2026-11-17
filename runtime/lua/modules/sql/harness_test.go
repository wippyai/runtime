// SPDX-License-Identifier: MPL-2.0

package sql

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/dispatcher"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/api/runtime/resource"
	"github.com/wippyai/runtime/runtime/lua/engine/value"
	sqlres "github.com/wippyai/runtime/service/sql"
)

const harnessPrelude = `
function check(v, msg)
	if not v then error(msg or "check failed", 2) end
	return v
end
function eq(a, b, msg)
	if a ~= b then error((msg or "eq") .. ": expected " .. tostring(b) .. ", got " .. tostring(a), 2) end
end
function kind_of(err, want, msg)
	check(err ~= nil, (msg or "error") .. ": expected " .. tostring(want) .. " error, got nil")
	eq(err:kind(), want, (msg or "error") .. " kind (" .. tostring(err) .. ")")
end
`

type harnessResult struct {
	data any
	err  error
}

type harnessReceiver chan harnessResult

func (r harnessReceiver) CompleteYield(_ uint64, data any, err error) { r <- harnessResult{data, err} }

// sqlEnv runs Lua scripts against the sql module and executes their yields on
// the SQL dispatcher, with a database handle bound to the global `db`. Cleanup
// cancels the dispatch context, which ends statements still running.
type sqlEnv struct {
	t        testing.TB
	l        *lua.LState
	ctx      context.Context
	handlers map[dispatcher.CommandID]dispatcher.Handler
	yields   int
}

// yieldWait bounds each dispatched command so a statement that ignores its
// deadline fails the test instead of hanging it.
const yieldWait = 10 * time.Second

func newSQLEnv(t testing.TB, db *sql.DB, dbType string) *sqlEnv {
	t.Helper()
	l := lua.NewState()
	lua.OpenErrors(l)
	mod, _ := Module.Build()
	l.SetGlobal("sql", mod)
	base, cancel := context.WithCancel(context.Background())
	ctx, fc := ctxapi.OpenFrameContext(base)
	store := resource.NewStore()
	require.NoError(t, resource.SetStore(ctx, store))
	l.SetContext(ctx)
	e := &sqlEnv{t: t, l: l, ctx: ctx, handlers: map[dispatcher.CommandID]dispatcher.Handler{}}
	sqlres.NewDispatcher().RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) { e.handlers[id] = h })
	l.SetGlobal("db", value.NewTypedUserData(l, NewDB(ctx, nil, db, dbType), dbTypeName))
	require.NoError(t, l.DoString(harnessPrelude))
	t.Cleanup(func() {
		cancel()
		l.Close()
		_ = store.Close()
		ctxapi.ReleaseFrameContext(fc)
	})
	return e
}

func (e *sqlEnv) exec(script string) ([]lua.LValue, error) {
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
		e.yields++
		h := e.handlers[cmd.CmdID()]
		require.NotNil(e.t, h, "no handler for %d", cmd.CmdID())
		r := make(harnessReceiver, 1)
		require.NoError(e.t, h.Handle(e.ctx, cmd, 1, r))
		var res harnessResult
		select {
		case res = <-r:
		case <-time.After(yieldWait):
			e.t.Fatalf("command %d did not complete within %s", cmd.CmdID(), yieldWait)
		}
		back := y.(luaapi.HandledYield).HandleResult(e.l, res.data, res.err)
		st, vals, err = e.l.Resume(co, nil, back...)
	}
	return vals, err
}

// run executes a script that must succeed.
func (e *sqlEnv) run(script string) {
	e.t.Helper()
	_, err := e.exec(script)
	require.NoError(e.t, err)
}
