// SPDX-License-Identifier: MPL-2.0

package sql

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/go-lua/types/diag"
	"github.com/wippyai/go-lua/types/io"
	"github.com/wippyai/runtime/runtime/lua/code"
)

func checkSQLTypes(t *testing.T, source string) []diag.Diagnostic {
	t.Helper()
	tc := code.NewTypeChecker(code.TypeCheckConfig{Enabled: true, Strict: true}, nil)
	_, diagnostics, err := tc.Check(source, "sql_options.lua", map[string]*io.Manifest{"sql": ModuleTypes()})
	require.NoError(t, err)
	return diagnostics
}

func TestStatementOptionsTypecheck(t *testing.T) {
	diagnostics := checkSQLTypes(t, `
local sql = require("sql")
local function run(db: sql.DB, opts: sql.StatementOptions)
    local rows, err = db:query("SELECT 1", nil, {timeout = "5s"})
    local res, err2 = db:execute("SELECT 1", {1}, opts)
    local plain, err3 = db:query("SELECT ?", {1})
    local tx, err4 = db:begin()
    if tx then
        local trows, err5 = tx:query("SELECT 1", nil, {timeout = "1s"})
        local tres, err6 = tx:execute("SELECT 1", nil, {timeout = "1s"})
    end
    local stmt, err7 = db:prepare("SELECT 1")
    if stmt then
        local srows, err8 = stmt:query(nil, {timeout = "1s"})
        local sres, err9 = stmt:execute({}, {timeout = "1s"})
    end
    local executor = sql.builder.select("1"):run_with(db)
    local brows, err10 = executor:query({timeout = "1s"})
    local bres, err11 = executor:exec({timeout = "1s"})
end
`)
	require.False(t, code.HasErrors(diagnostics), "unexpected diagnostics: %v", diagnostics)
}

func TestStatementOptionsTypecheckRejectsNumericTimeout(t *testing.T) {
	diagnostics := checkSQLTypes(t, `
local sql = require("sql")
local function run(db: sql.DB)
    local rows, err = db:query("SELECT 1", nil, {timeout = 5})
end
`)
	require.True(t, code.HasErrors(diagnostics), "numeric timeout passed the type check")
}
