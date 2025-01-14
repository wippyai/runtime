package sql

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"

	lengine "github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/yuin/gopher-lua"
	"go.uber.org/zap"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/require"
)

func RunLuaSQLTest(luaCode, testName string) (any, error) {
	log := zap.NewNop()
	LE := lengine.NewCVM(context.Background(), log.Named("tests"))
	LE.L.PreloadModule("sql", NewModule(log.Named("sql")).Loader)

	err := LE.Init(luaCode, testName)
	if err != nil {
		return nil, err
	}

	tres := LE.Get(-1)

	var errv lua.LValue
	if LE.L.GetTop() == 2 {
		errv = LE.Get(-1)
	}

	if errv != nil {
		return nil, errors.New(errv.String())
	}

	return lengine.ToGoAny(tres), nil
}

func TestLuaLNil(t *testing.T) {
	db, err := sql.Open("sqlite3", "./test.db")
	require.NoError(t, err)
	err = db.Ping()

	require.NoError(t, err)
	_ = db.Close()
	t.Cleanup(func() {
		cleanupDB()
	})
}

// test connect from lua
func TestLuaConnect(t *testing.T) {
	luaCode := `
	local sql = require("sql")
	local db, err = sql.connect("sqlite3", "test.db")
	if err then
		return nil, err
	end

	db:close()
	`

	db, err := RunLuaSQLTest(luaCode, "TestLuaConnect")
	require.NoError(t, err)
	require.NotNil(t, db)

	t.Cleanup(func() {
		cleanupDB()
	})
}

// Test executing a simple query from Lua
func TestLuaExecuteQuery(t *testing.T) {
	// Setup the database
	db, err := sql.Open("sqlite3", "test.db")
	require.NoError(t, err)
	defer func() {
		_ = db.Close()
	}()

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS users(id INTEGER PRIMARY KEY, name TEXT)`)
	require.NoError(t, err)

	luaCode := `
	local sql = require("sql")
	local db, err = sql.connect("sqlite3", "test.db")
	if err then
		return nil, err
	end

	local result, err = db:execute("INSERT INTO users (name) VALUES ('Alice')")
	if err then
		return nil, err
	end

	return result
	`

	result, err := RunLuaSQLTest(luaCode, "TestLuaExecuteQuery")
	require.NoError(t, err)
	require.NotNil(t, result)
	t.Cleanup(func() {
		cleanupDB()
	})
}

// Test executing a simple query from Lua
func TestLuaExecuteQueryWithArgs(t *testing.T) {
	// Setup the database
	db, err := sql.Open("sqlite3", "test.db")
	require.NoError(t, err)
	defer func() {
		_ = db.Close()
	}()

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS users(id INTEGER PRIMARY KEY, name TEXT)`)
	require.NoError(t, err)

	luaCode := `
	local sql = require("sql")
	local db, err = sql.connect("sqlite3", "test.db")
	if err then
		return nil, err
	end

	local result, err = db:execute("INSERT INTO users (id, name) VALUES (?, ?)", {99, "Alice"})
	if err then
		return nil, err
	end

	return result.last_insert_id
	`

	result, err := RunLuaSQLTest(luaCode, "TestLuaExecuteQueryWithArgs")
	require.NoError(t, err)
	require.Equal(t, float64(99), result)
	t.Cleanup(func() {
		cleanupDB()
	})
}

// Test querying data from Lua
func TestLuaQueryData(t *testing.T) {
	// Setup the database
	db, err := sql.Open("sqlite3", "test.db")
	require.NoError(t, err)
	defer func() {
		_ = db.Close()
	}()

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS users(id INTEGER PRIMARY KEY, name TEXT)`)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO users(name) VALUES('Bob')`)
	require.NoError(t, err)

	_, err = db.Exec(`INSERT INTO users(name) VALUES('John')`)
	require.NoError(t, err)

	luaCode := `
	local sql = require("sql")
	local db, err = sql.connect("sqlite3", "test.db")
	if err then
		return nil, err
	end

	local rows, err = db:query("SELECT name FROM users WHERE id IN (?, ?) ORDER BY id DESC", {2, 1})
	if err then
		return nil, err
	end

	return {rows[1].name, rows[2].name}
	`

	result, err := RunLuaSQLTest(luaCode, "TestLuaQueryData")
	require.NoError(t, err)
	require.NotNil(t, result)

	require.Equal(t, []any{"John", "Bob"}, result)
	t.Cleanup(func() {
		cleanupDB()
	})
}

// Test handling of SQL errors in Lua
func TestLuaSQLErrorHandling(t *testing.T) {
	luaCode := `
	local sql = require("sql")
	local db, err = sql.connect("sqlite3", "test.db")
	if err then
		return nil, err
	end

	-- This should fail because the table does not exist
	local result, err = db:Execute("INSERT INTO nonexistent_table (name) VALUES ('Charlie')")
	if err then
		return nil, err
	end

	return result
	`

	result, err := RunLuaSQLTest(luaCode, "TestLuaSQLErrorHandling")
	require.Error(t, err)
	require.Nil(t, result)
	t.Cleanup(func() {
		cleanupDB()
	})
}

// Test committing a transaction in Lua
func TestLuaTransactionCommit(t *testing.T) {
	// set up the database
	db, err := sql.Open("sqlite3", "test.db")
	require.NoError(t, err)
	defer func() {
		_ = db.Close()
	}()

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS accounts(id INTEGER PRIMARY KEY, balance INTEGER)`)
	require.NoError(t, err)

	// Initialize an account with balance 100
	_, err = db.Exec(`INSERT INTO accounts(id, balance) VALUES (1, 100)`)
	require.NoError(t, err)

	luaCode := `
	local sql = require("sql")
	local db, err = sql.connect("sqlite3", "test.db")
	if err then
		return nil, err
	end

	local err = db:begin()
	if err then
		return nil, err
	end

	local res, err = db:execute("UPDATE accounts SET balance = balance - 50 WHERE id = ?", {1})
	if err then
		db:rollback()
		return nil, err
	end

	local res2, err = db:execute("UPDATE accounts SET balance = balance + 50 WHERE id = ?", {2})
	if err then
		db:rollback()
		return nil, err
	end

	err = db:commit()
	if err ~= nil then
		return nil, err
	end

	return "Transaction Committed"
	`

	result, err := RunLuaSQLTest(luaCode, "TestLuaTransactionCommit")
	require.Error(t, err) // Because account with id=2 doesn't exist
	require.Nil(t, result)

	// Verify that balance remains unchanged due to rollback
	var balance int
	err = db.QueryRow(`SELECT balance FROM accounts WHERE id = 1`).Scan(&balance)
	require.NoError(t, err)
	require.Equal(t, 100, balance)
	t.Cleanup(func() {
		cleanupDB()
	})
}

// Test rolling back a transaction in Lua
func TestLuaTransactionRollback(t *testing.T) {
	// Setup the database
	db, err := sql.Open("sqlite3", "test.db")
	require.NoError(t, err)
	defer func() {
		_ = db.Close()
	}()

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS accounts(id INTEGER PRIMARY KEY, balance INTEGER)`)
	require.NoError(t, err)

	// Initialize account with balance 100
	_, err = db.Exec(`INSERT OR IGNORE INTO accounts(id, balance) VALUES (2, 100)`)
	require.NoError(t, err)

	luaCode := `
	local sql = require("sql")
	local db, err = sql.connect("sqlite3", "test.db")
	if err then
		return nil, err
	end

	local err = db:begin()
	if err then
		return nil, err
	end

	local res, err = db:execute("UPDATE accounts SET balance = balance - 30 WHERE id = ?", {2})
	if err ~= nil {
		db:rollback()
		return nil, err
	end

	-- Intentional error to trigger rollback
	local res2, err = db:execute("UPDATE nonexistent_table SET balance = balance + 30 WHERE id = ?", {1})
	if err ~= nil then
		db:rollback()
		return nil, err
	end

	err = db:commit()
	if err ~= nil then
		return nil, err
	end

	return "Transaction Committed"
	`

	result, err := RunLuaSQLTest(luaCode, "TestLuaTransactionRollback")
	require.Error(t, err)
	require.Nil(t, result)

	// Verify that balance remains unchanged due to rollback
	var balance int
	err = db.QueryRow(`SELECT balance FROM accounts WHERE id = 2`).Scan(&balance)
	require.NoError(t, err)
	require.Equal(t, 100, balance)
	t.Cleanup(func() {
		cleanupDB()
	})
}

// Test committing a transaction successfully
func TestLuaTransactionSuccess(t *testing.T) {
	// Setup the database
	db, err := sql.Open("sqlite3", "test.db")
	require.NoError(t, err)
	defer func() {
		_ = db.Close()
	}()

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS transactions(id INTEGER PRIMARY KEY, amount INTEGER)`)
	require.NoError(t, err)

	luaCode := `
	local sql = require("sql")
	local db, err = sql.connect("sqlite3", "test.db")
	if err then
		return nil, err
	end

	local err = db:beginTransaction()
	if err then
		return nil, err
	end

	local res, err = db:execute("INSERT INTO transactions(amount) VALUES (?)", {200})
    if err ~= nil then
		db:rollback()
		return nil, err
	end

	err = db:commit()
	if err ~= nil then
		return nil, err
	end

	return "Transaction Committed Successfully"
	`

	result, err := RunLuaSQLTest(luaCode, "TestLuaTransactionSuccess")
	require.NoError(t, err)
	require.Equal(t, "Transaction Committed Successfully", result)

	// Verify that the transaction was committed
	var amount int
	err = db.QueryRow(`SELECT amount FROM transactions WHERE id = 1`).Scan(&amount)
	require.NoError(t, err)
	require.Equal(t, 200, amount)
	t.Cleanup(func() {
		cleanupDB()
	})
}

// Test handling of nested transactions in Lua // TODO: maybe let lua work write to transaction directly as go?
func TestLuaNestedTransactions(t *testing.T) {
	t.Skip("we do not support nested transactions atm")
	// Setup the database
	db, err := sql.Open("sqlite3", "test.db")
	require.NoError(t, err)
	defer func() {
		_ = db.Close()
	}()

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS nested(id INTEGER PRIMARY KEY, value TEXT)`)
	require.NoError(t, err)

	luaCode := `
local sql = require("sql")
local db, err = sql.connect("sqlite3://./test.db")
if err then
	return nil, err
end

-- Begin outer transaction
local err = db:beginTransaction()
if err ~= nil then
	return nil, err
end

local res1, err = db:execute("INSERT INTO nested(value) VALUES (?)", {"Outer"})
if err ~= nil then
	db:rollback()
	return nil, err
end

-- Begin nested transaction
local err = db:beginTransaction()
if err ~= nil then
	db:rollback()
	return nil, err
end

local res2, err = db:execute("INSERT INTO nested(value) VALUES (?)", {"Inner"})
if err ~= nil then
	db:rollback()
	return nil, err
end

err = db:commit()
if err ~= nil then
	db:rollback()
	return nil, err
end

return "Nested Transaction Committed"
`

	result, err := RunLuaSQLTest(luaCode, "TestLuaNestedTransactions")
	require.NoError(t, err)
	require.Equal(t, "Nested Transaction Committed", result)

	// Verify both transactions were committed
	var value1, value2 string
	err = db.QueryRow(`SELECT value FROM nested WHERE id = 1`).Scan(&value1)
	require.NoError(t, err)
	require.Equal(t, "Outer", value1)

	err = db.QueryRow(`SELECT value FROM nested WHERE id = 2`).Scan(&value2)
	require.NoError(t, err)
	require.Equal(t, "Inner", value2)

	t.Cleanup(func() {
		cleanupDB()
	})
}

// Test transaction error handling in Lua
func TestLuaTransactionErrorHandling(t *testing.T) {
	// Setup the database
	db, err := sql.Open("sqlite3", "test.db")
	require.NoError(t, err)
	defer func() {
		_ = db.Close()
	}()

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS error_handling(id INTEGER PRIMARY KEY, data TEXT)`)
	require.NoError(t, err)

	luaCode := `
	local sql = require("sql")
	local db, err = sql.connect("sqlite3", "test.db")
	if err then
		return nil, err
	end

	local err = db:beginTransaction()
	if err then
		return nil, err
	end

	local res, err = db:execute("INSERT INTO error_handling(data) VALUES (?)", {"Test"})
	if err then
		db:rollback()
		return nil, err
	end

	-- Intentional error
	local res2, err = db:execute("INSERT INTO non_existing_table(data) VALUES (?)", {"Error"})
	if err ~= nil then
		db:rollback()
		return nil, err
	end

	err = db:commit()
	if err ~= nil then
		return nil, err
	end

	return "Transaction Completed"
	`

	result, err := RunLuaSQLTest(luaCode, "TestLuaTransactionErrorHandling")
	require.Error(t, err)
	require.Nil(t, result)

	// Verify that the transaction was rolled back
	var count int
	err = db.QueryRow(`SELECT COUNT(*) FROM error_handling`).Scan(&count)
	require.NoError(t, err)
	require.Equal(t, 0, count)
	t.Cleanup(func() {
		cleanupDB()
	})
}

// Test handling invalid parameters in Lua
func TestLuaInvalidParameters(t *testing.T) {
	luaCode := `
	local sql = require("sql")
	local db, err = sql.connect("sqlite3", "test.db")
	if err then
		return nil, err
	end

	-- Passing a table instead of a string for the query
	local res, err = db:execute({"INVALID QUERY"}, {"param"})
	if err then
		return nil, err
	end

	return res
	`

	result, err := RunLuaSQLTest(luaCode, "TestLuaInvalidParameters")
	require.Error(t, err)
	require.Nil(t, result)
	t.Cleanup(func() {
		cleanupDB()
	})
}

// Test concurrent transactions in Lua
func TestLuaConcurrentTransactions(t *testing.T) {
	// Setup the database
	db, err := sql.Open("sqlite3", "test.db")
	require.NoError(t, err)
	defer func() {
		_ = db.Close()
	}()

	_, err = db.Exec(`CREATE TABLE IF NOT EXISTS concurrent(id INTEGER PRIMARY KEY, count INTEGER)`)
	require.NoError(t, err)

	// Initialize count to 0
	_, err = db.Exec(`INSERT OR IGNORE INTO concurrent(id, count) VALUES (1, 0)`)
	require.NoError(t, err)

	luaCode1 := `
	local sql = require("sql")
	local db, err = sql.connect("sqlite3", "test.db")
	if err then
		return nil, err
	end

	local err = db:beginTransaction()
	if err then
		return nil, err
	end

	local res, err = db:execute("UPDATE concurrent SET count = count + 1 WHERE id = ?", {1})
	if err ~= nil then
		db:rollback()
		return nil, err
	end

	err = db:commit()
	if err ~= nil then
		return nil, err
	end

	return "Transaction 1 Committed"
	`

	luaCode2 := `
	local sql = require("sql")
	local db, err = sql.connect("sqlite3", "test.db")
	if err then
		return nil, err
	end

	local err = db:beginTransaction()
	if err then
		return nil, err
	end

	local res, err = db:execute("UPDATE concurrent SET count = count + 1 WHERE id = ?", {1})
	if err ~= nil then
		db:rollback()
		return nil, err
	end

	err = db:commit()
	if err ~= nil then
		return nil, err
	end

	return "Transaction 2 Committed"
	`

	// Run both transactions
	result1, err1 := RunLuaSQLTest(luaCode1, "TestLuaConcurrentTransactions1")
	result2, err2 := RunLuaSQLTest(luaCode2, "TestLuaConcurrentTransactions2")

	require.NoError(t, err1)
	require.Equal(t, "Transaction 1 Committed", result1)

	require.NoError(t, err2)
	require.Equal(t, "Transaction 2 Committed", result2)

	// Verify the count is 2
	var count int
	err = db.QueryRow(`SELECT count FROM concurrent WHERE id = 1`).Scan(&count)
	require.NoError(t, err)
	require.Equal(t, 2, count)

	t.Cleanup(func() {
		cleanupDB()
	})
}

func cleanupDB() {
	// unlink file if exists
	if _, err := os.Stat("test.db"); err == nil {
		err = os.Remove("test.db")
		if err != nil {
			panic(err)
		}
	}
}
