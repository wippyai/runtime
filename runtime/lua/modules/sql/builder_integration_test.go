package sql

import (
	"database/sql"
	"testing"

	sqlapi "github.com/ponyruntime/pony/api/service/sql"
	sqlres "github.com/ponyruntime/pony/service/sql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	lua "github.com/yuin/gopher-lua"

	_ "github.com/mattn/go-sqlite3"
)

// TestSelectBuilderWithDB tests using a SelectBuilder with a real DB
func TestSelectBuilderWithDB(t *testing.T) {
	// Setup test database with table and data
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	defer db.Close()

	_, err = db.Exec(`CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT, active INTEGER)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO users VALUES (1, 'Alice', 1), (2, 'Bob', 0), (3, 'Charlie', 1)`)
	require.NoError(t, err)

	// Create mock resource
	mockRes := &mockResource{
		resValue: sqlres.DBResource{
			DB:   db,
			Type: sqlapi.KindSQLite,
		},
	}

	// Setup Lua environment
	vm, L, uw, runner := setupLuaWithDB(t, mockRes)
	defer vm.Close()
	defer func() { _ = uw.Close() }()

	// Test script
	script := `
		function test_select()
			local sql = require("sql")
			local db, err = sql.get("app:test_db")
			if err then error(err) end

			-- SELECT with WHERE and ORDER BY
			local query = sql.builder.select("id", "name")
				:from("users")
				:where({active = 1})
				:order_by("name ASC")

			local executor = query:run_with(db)
			local rows, err = executor:query()
			if err then error(err) end

			return {count = #rows, first_name = rows[1].name}
		end
	`

	err = vm.Import(script, "test", "test_select")
	require.NoError(t, err)

	result, err := runner.Execute(L.Context(), "test_select")
	require.NoError(t, err)
	resultTable := result.(*lua.LTable)

	count := resultTable.RawGetString("count")
	assert.Equal(t, lua.LNumber(2), count, "Should find 2 active users")
}

// TestInsertBuilderWithDB tests using an InsertBuilder with a real DB
func TestInsertBuilderWithDB(t *testing.T) {
	// Setup test database
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	defer db.Close()

	_, err = db.Exec(`CREATE TABLE items (id INTEGER PRIMARY KEY, name TEXT, price REAL)`)
	require.NoError(t, err)

	// Create mock resource
	mockRes := &mockResource{
		resValue: sqlres.DBResource{
			DB:   db,
			Type: sqlapi.KindSQLite,
		},
	}

	// Setup Lua environment
	vm, L, uw, runner := setupLuaWithDB(t, mockRes)
	defer vm.Close()
	defer func() { _ = uw.Close() }()

	// Test script
	script := `
		function test_insert()
			local sql = require("sql")
			local db, err = sql.get("app:test_db")
			if err then error(err) end

			-- INSERT with columns/values
			local insert = sql.builder.insert("items")
				:columns("name", "price")
				:values("Test Item", 9.99)

			local executor = insert:run_with(db)
			local result, err = executor:exec()
			if err then error(err) end

			-- Verify with SELECT
			local rows, err = db:query("SELECT * FROM items WHERE name = 'Test Item'")
			if err then error(err) end

			return {
				rows_affected = result.rows_affected,
				found = #rows > 0
			}
		end
	`

	err = vm.Import(script, "test", "test_insert")
	require.NoError(t, err)

	result, err := runner.Execute(L.Context(), "test_insert")
	require.NoError(t, err)
	resultTable := result.(*lua.LTable)

	rowsAffected := resultTable.RawGetString("rows_affected")
	found := resultTable.RawGetString("found")

	assert.Equal(t, lua.LNumber(1), rowsAffected, "Should affect 1 row")
	assert.Equal(t, lua.LBool(true), found, "Should find inserted row")
}

// TestUpdateBuilderWithDB tests using an UpdateBuilder with a real DB
func TestUpdateBuilderWithDB(t *testing.T) {
	// Setup test database
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	defer db.Close()

	_, err = db.Exec(`CREATE TABLE products (id INTEGER PRIMARY KEY, name TEXT, price REAL, active INTEGER)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO products VALUES (1, 'Old Name', 10, 1)`)
	require.NoError(t, err)

	// Create mock resource
	mockRes := &mockResource{
		resValue: sqlres.DBResource{
			DB:   db,
			Type: sqlapi.KindSQLite,
		},
	}

	// Setup Lua environment
	vm, L, uw, runner := setupLuaWithDB(t, mockRes)
	defer vm.Close()
	defer func() { _ = uw.Close() }()

	// Test script
	script := `
		function test_update()
			local sql = require("sql")
			local db, err = sql.get("app:test_db")
			if err then error(err) end

			-- UPDATE with WHERE
			local update = sql.builder.update("products")
				:set("name", "New Name")
				:set("price", 15.99)
				:where({id = 1})

			local executor = update:run_with(db)
			local result, err = executor:exec()
			if err then error(err) end

			-- Verify with SELECT
			local rows, err = db:query("SELECT name, price FROM products WHERE id = 1")
			if err then error(err) end

			return {
				rows_affected = result.rows_affected,
				new_name = rows[1].name,
				new_price = rows[1].price
			}
		end
	`

	err = vm.Import(script, "test", "test_update")
	require.NoError(t, err)

	result, err := runner.Execute(L.Context(), "test_update")
	require.NoError(t, err)
	resultTable := result.(*lua.LTable)

	rowsAffected := resultTable.RawGetString("rows_affected")
	newName := resultTable.RawGetString("new_name")
	newPrice := resultTable.RawGetString("new_price")

	assert.Equal(t, lua.LNumber(1), rowsAffected, "Should affect 1 row")
	assert.Equal(t, lua.LString("New Name"), newName, "Name should be updated")
	assert.Equal(t, lua.LNumber(15.99), newPrice, "Price should be updated")
}

// TestDeleteBuilderWithDB tests using a DeleteBuilder with a real DB
func TestDeleteBuilderWithDB(t *testing.T) {
	// Setup test database
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	defer db.Close()

	_, err = db.Exec(`CREATE TABLE contacts (id INTEGER PRIMARY KEY, name TEXT, active INTEGER)`)
	require.NoError(t, err)
	_, err = db.Exec(`INSERT INTO contacts VALUES 
		(1, 'Keep', 1),
		(2, 'Delete', 0),
		(3, 'Keep Too', 1)`)
	require.NoError(t, err)

	// Create mock resource
	mockRes := &mockResource{
		resValue: sqlres.DBResource{
			DB:   db,
			Type: sqlapi.KindSQLite,
		},
	}

	// Setup Lua environment
	vm, L, uw, runner := setupLuaWithDB(t, mockRes)
	defer vm.Close()
	defer func() { _ = uw.Close() }()

	// Test script
	script := `
		function test_delete()
			local sql = require("sql")
			local db, err = sql.get("app:test_db")
			if err then error(err) end

			-- Count before delete
			local before_rows, err = db:query("SELECT COUNT(*) as count FROM contacts")
			if err then error(err) end
			local before_count = before_rows[1].count

			-- DELETE with WHERE
			local delete = sql.builder.delete("contacts")
				:where({active = 0})

			local executor = delete:run_with(db)
			local result, err = executor:exec()
			if err then error(err) end

			-- Count after delete
			local after_rows, err = db:query("SELECT COUNT(*) as count FROM contacts")
			if err then error(err) end
			local after_count = after_rows[1].count

			return {
				rows_affected = result.rows_affected,
				before_count = before_count,
				after_count = after_count
			}
		end
	`

	err = vm.Import(script, "test", "test_delete")
	require.NoError(t, err)

	result, err := runner.Execute(L.Context(), "test_delete")
	require.NoError(t, err)
	resultTable := result.(*lua.LTable)

	rowsAffected := resultTable.RawGetString("rows_affected")
	beforeCount := resultTable.RawGetString("before_count")
	afterCount := resultTable.RawGetString("after_count")

	assert.Equal(t, lua.LNumber(1), rowsAffected, "Should affect 1 row")
	assert.Equal(t, lua.LNumber(3), beforeCount, "Should have 3 rows before")
	assert.Equal(t, lua.LNumber(2), afterCount, "Should have 2 rows after")
}
