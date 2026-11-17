//go:build cgo

// SPDX-License-Identifier: MPL-2.0

package sql

import (
	"database/sql"
	"testing"
	"time"

	_ "github.com/rqlite/go-sqlite3"
	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	sqlapi "github.com/wippyai/runtime/api/service/sql"
)

// endlessCount never finishes on its own; only an interrupt ends it.
const endlessCount = `WITH RECURSIVE c(x) AS (SELECT 1 UNION ALL SELECT x + 1 FROM c) SELECT max(x) AS n FROM c`

// interruptBound is how long a statement with a 100ms timeout may take to
// return; it leaves room for a loaded race-enabled run.
const interruptBound = 3 * time.Second

func newSQLiteTimeoutEnv(t *testing.T) *sqlEnv {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.ExecContext(t.Context(), `CREATE TABLE sink (n INTEGER)`)
	require.NoError(t, err)
	return newSQLEnv(t, db, sqlapi.SQLite)
}

func (e *sqlEnv) runTimed(script string) time.Duration {
	e.t.Helper()
	start := time.Now()
	e.run(script)
	return time.Since(start)
}

func TestStatementTimeoutInterruptsSQLite(t *testing.T) {
	cases := map[string]string{
		"db query": `
			local rows, err = db:query(QUERY, nil, {timeout = "100ms"})
			eq(rows, nil, "rows")
			kind_of(err, errors.TIMEOUT, "query")
			eq(err:retryable(), false, "retryable")`,
		"db execute": `
			local res, err = db:execute("INSERT INTO sink " .. QUERY, {}, {timeout = "100ms"})
			eq(res, nil, "result")
			kind_of(err, errors.TIMEOUT, "execute")`,
		"tx query": `
			local tx = check(db:begin())
			local rows, err = tx:query(QUERY, nil, {timeout = "100ms"})
			eq(rows, nil, "rows")
			kind_of(err, errors.TIMEOUT, "tx query")
			check(tx:rollback())`,
		"tx execute": `
			local tx = check(db:begin())
			local res, err = tx:execute("INSERT INTO sink " .. QUERY, nil, {timeout = "100ms"})
			eq(res, nil, "result")
			kind_of(err, errors.TIMEOUT, "tx execute")
			-- SQLite rolls back a transaction whose write was interrupted, so
			-- rollback only releases the connection here.
			tx:rollback()`,
		"statement query": `
			local stmt = check(db:prepare(QUERY))
			local rows, err = stmt:query(nil, {timeout = "100ms"})
			eq(rows, nil, "rows")
			kind_of(err, errors.TIMEOUT, "statement query")
			check(stmt:close())`,
		"statement execute": `
			local stmt = check(db:prepare("INSERT INTO sink " .. QUERY))
			local res, err = stmt:execute({}, {timeout = "100ms"})
			eq(res, nil, "result")
			kind_of(err, errors.TIMEOUT, "statement execute")
			check(stmt:close())`,
		"builder query": `
			local executor = check(sql.builder.select("max(x) AS n"):from(ENDLESS_SOURCE):run_with(db))
			local rows, err = executor:query({timeout = "100ms"})
			eq(rows, nil, "rows")
			kind_of(err, errors.TIMEOUT, "builder query")`,
		"builder exec": `
			local executor = check(sql.builder.insert("sink"):columns("n")
				:select(sql.builder.select("max(x)"):from(ENDLESS_SOURCE))
				:run_with(db))
			local res, err = executor:exec({timeout = "100ms"})
			eq(res, nil, "result")
			kind_of(err, errors.TIMEOUT, "builder exec")`,
	}
	for name, script := range cases {
		t.Run(name, func(t *testing.T) {
			e := newSQLiteTimeoutEnv(t)
			e.l.SetGlobal("QUERY", lua.LString(endlessCount))
			e.l.SetGlobal("ENDLESS_SOURCE", lua.LString("(WITH RECURSIVE c(x) AS (SELECT 1 UNION ALL SELECT x + 1 FROM c) SELECT x FROM c)"))
			elapsed := e.runTimed(script)
			require.Less(t, elapsed, interruptBound, "statement was not interrupted promptly")
			e.run(`
				local rows, err = db:query("SELECT count(*) AS n FROM sink")
				eq(err, nil, "database usable after timeout")
				eq(rows[1].n, 0, "interrupted write left no rows")`)
		})
	}
}

func TestStatementTimeoutAllowsCompletion(t *testing.T) {
	e := newSQLiteTimeoutEnv(t)
	e.run(`
		local res, err = db:execute("INSERT INTO sink VALUES (?)", {7}, {timeout = "5s"})
		eq(err, nil, "execute")
		eq(res.rows_affected, 1, "rows affected")
		local rows, err2 = db:query("SELECT n FROM sink", nil, {timeout = "5s"})
		eq(err2, nil, "query")
		eq(rows[1].n, 7, "value")
		local tx = check(db:begin())
		check(tx:execute("INSERT INTO sink VALUES (8)", nil, {timeout = "5s"}))
		eq(#check(tx:query("SELECT n FROM sink", {}, {timeout = "5s"})), 2, "tx rows")
		check(tx:commit())
		local stmt = check(db:prepare("SELECT n FROM sink WHERE n = ?"))
		eq(check(stmt:query({8}, {timeout = "5s"}))[1].n, 8, "statement row")
		check(stmt:close())
		local executor = check(sql.builder.select("n"):from("sink"):run_with(db))
		eq(#check(executor:query({timeout = "5s"})), 2, "builder rows")`)
}

func TestStatementWithoutOptionsKeepsBehavior(t *testing.T) {
	e := newSQLiteTimeoutEnv(t)
	e.run(`
		check(db:execute("INSERT INTO sink VALUES (1)"))
		check(db:execute("INSERT INTO sink VALUES (?)", {2}))
		check(db:execute("INSERT INTO sink VALUES (?)", {3}, nil))
		eq(#check(db:query("SELECT n FROM sink")), 3, "rows")
		eq(#check(db:query("SELECT n FROM sink WHERE n > ?", {1}, nil)), 2, "rows with nil options")
		local rows = check(db:query("WITH RECURSIVE c(x) AS (SELECT 1 UNION ALL SELECT x + 1 FROM c WHERE x < 200000) SELECT max(x) AS n FROM c"))
		eq(rows[1].n, 200000, "bounded count")`)
}

func TestStatementOptionsRejectInvalidValues(t *testing.T) {
	calls := map[string]string{
		"db query":          `return db:query("SELECT 1", nil, OPTIONS)`,
		"db execute":        `return db:execute("SELECT 1", nil, OPTIONS)`,
		"tx query":          `local tx = check(db:begin()); local r, e = tx:query("SELECT 1", nil, OPTIONS); check(tx:rollback()); return r, e`,
		"tx execute":        `local tx = check(db:begin()); local r, e = tx:execute("SELECT 1", nil, OPTIONS); check(tx:rollback()); return r, e`,
		"statement query":   `local s = check(db:prepare("SELECT 1")); local r, e = s:query(nil, OPTIONS); check(s:close()); return r, e`,
		"statement execute": `local s = check(db:prepare("SELECT 1")); local r, e = s:execute(nil, OPTIONS); check(s:close()); return r, e`,
		"builder query":     `return check(sql.builder.select("1"):run_with(db)):query(OPTIONS)`,
		"builder exec":      `return check(sql.builder.delete("sink"):run_with(db)):exec(OPTIONS)`,
	}
	options := map[string]string{
		"not a table":      `"5s"`,
		"number timeout":   `{timeout = 5}`,
		"boolean timeout":  `{timeout = true}`,
		"malformed":        `{timeout = "soon"}`,
		"zero":             `{timeout = "0s"}`,
		"negative":         `{timeout = "-1s"}`,
		"unknown option":   `{deadline = "1s"}`,
		"positional entry": `{"1s"}`,
	}
	for call, body := range calls {
		for option, literal := range options {
			t.Run(call+"/"+option, func(t *testing.T) {
				e := newSQLiteTimeoutEnv(t)
				e.run(`
					OPTIONS = ` + literal + `
					local result, err = (function() ` + body + ` end)()
					eq(result, nil, "result")
					kind_of(err, errors.INVALID, "options")
					eq(err:retryable(), false, "retryable")`)
			})
		}
	}
}
