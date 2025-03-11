# SQL Package Specification

## Overview

The SQL package provides a Lua interface to SQL databases. It exposes a consistent API for database operations that
works across different database engines (PostgreSQL, SQLite, etc.).

## Module Interface

### Loading the Module

```lua
local sql = require("sql")
```

### Constants

#### Database Type Constants

Available in `sql.type`:

- `postgres` (string): PostgreSQL database
- `mysql` (string): MySQL database
- `sqlite` (string): SQLite database
- `mssql` (string): Microsoft SQL Server database
- `oracle` (string): Oracle database
- `unknown` (string): Unknown or unsupported database type

## Core Concepts

### Resource Management

- Database connections are obtained from the resource registry
- All resources (connections, statements, transactions) are automatically cleaned up when the containing unit of work
  completes
- Resources can be explicitly released earlier if needed

### Error Handling

- Most operations return nil + error message on failure
- Success is indicated by a result value + nil
- Database connections are managed as resources with proper cleanup

## Database Operations

### Getting a Database Connection

```lua
local db, err = sql.get("resource_id")
-- Parameters: resource_id (string) - Resource ID for the database
-- Returns on success: database object, nil
-- Returns on error: nil, error message
```

### Execute Query

```lua
local result, err = db:query(sql_query[, params])
-- Parameters:
--   sql_query (string): SQL query to execute
--   params (table, optional): Array of parameter values
-- Returns on success: table of result rows, nil
-- Returns on error: nil, error message
-- Note: Each row is a table with column names as keys
```

### Execute Update/Insert/Delete

```lua
local result, err = db:execute(sql_statement[, params])
-- Parameters:
--   sql_statement (string): SQL statement to execute
--   params (table, optional): Array of parameter values
-- Returns on success: result table, nil
--   result.rows_affected: Number of rows affected
--   result.last_insert_id: Last insert ID (if applicable)
-- Returns on error: nil, error message
```

### Prepare Statement

```lua
local stmt, err = db:prepare(sql_query)
-- Parameters: sql_query (string) - SQL query to prepare
-- Returns on success: statement object, nil
-- Returns on error: nil, error message
```

### Begin Transaction

```lua
local tx, err = db:begin()
-- Returns on success: transaction object, nil
-- Returns on error: nil, error message
```

### Get Database Type

```lua
local type, err = db:type()
-- Returns on success: string representing the database type, nil
-- Returns on error: nil, error message
-- Note: The returned type will be one of the constants in sql.type
```

### Release Connection

```lua
local ok, err = db:release()
-- Returns on success: true, nil
-- Returns on error: nil, error message
```

## Prepared Statement Operations

### Execute Prepared Query

```lua
local rows, err = stmt:query([params])
-- Parameters: params (table, optional) - Array of parameter values
-- Returns on success: table of result rows, nil
-- Returns on error: nil, error message
```

### Execute Prepared Statement

```lua
local result, err = stmt:execute([params])
-- Parameters: params (table, optional) - Array of parameter values
-- Returns on success: result table, nil
--   result.rows_affected: Number of rows affected
--   result.last_insert_id: Last insert ID (if applicable)
-- Returns on error: nil, error message
```

### Close Statement

```lua
local ok, err = stmt:close()
-- Returns on success: true, nil
-- Returns on error: nil, error message
```

## Transaction Operations

### Execute Query in Transaction

```lua
local rows, err = tx:query(sql_query[, params])
-- Parameters:
--   sql_query (string): SQL query to execute
--   params (table, optional): Array of parameter values
-- Returns on success: table of result rows, nil
-- Returns on error: nil, error message
```

### Execute Statement in Transaction

```lua
local result, err = tx:execute(sql_statement[, params])
-- Parameters:
--   sql_statement (string): SQL statement to execute
--   params (table, optional): Array of parameter values
-- Returns on success: result table, nil
--   result.rows_affected: Number of rows affected
--   result.last_insert_id: Last insert ID (if applicable)
-- Returns on error: nil, error message
```

### Prepare Statement in Transaction

```lua
local stmt, err = tx:prepare(sql_query)
-- Parameters: sql_query (string) - SQL query to prepare
-- Returns on success: statement object, nil
-- Returns on error: nil, error message
```

### Create Savepoint

```lua
local ok, err = tx:savepoint(name)
-- Parameters: name (string) - Savepoint name
-- Returns on success: true, nil
-- Returns on error: nil, error message
```

### Rollback to Savepoint

```lua
local ok, err = tx:rollback_to(name)
-- Parameters: name (string) - Savepoint name
-- Returns on success: true, nil
-- Returns on error: nil, error message
```

### Release Savepoint

```lua
local ok, err = tx:release(name)
-- Parameters: name (string) - Savepoint name
-- Returns on success: true, nil
-- Returns on error: nil, error message
```

### Commit Transaction

```lua
local ok, err = tx:commit()
-- Returns on success: true, nil
-- Returns on error: nil, error message
```

### Rollback Transaction

```lua
local ok, err = tx:rollback()
-- Returns on success: true, nil
-- Returns on error: nil, error message
```

## Example Usage

### Database Type Check

```lua
-- Get database connection from resource registry
local db, err = sql.get("main_db")
if err then error(err) end

-- Check database type
local dbType, err = db:type()
if err then error(err) end

-- Use type-specific features
if dbType == sql.type.postgres then
    -- Use PostgreSQL-specific features
elseif dbType == sql.type.sqlite then
    -- Use SQLite-specific features
else
    print("Using generic SQL features for " .. dbType)
end
```

### Basic Query Operations

```lua
-- Get database connection from resource registry
local db, err = sql.get("main_db")
if err then error(err) end

-- Execute a simple query
local users, err = db:query("SELECT * FROM users WHERE active = ?", {true})
if err then error(err) end

-- Process results
for i, user in ipairs(users) do
    print(string.format("User %d: %s (%s)", i, user.name, user.email))
end

-- Release the database when done
db:release()
```

### Transaction Example

```lua
-- Get database connection
local db, err = sql.get("main_db")
if err then error(err) end

-- Begin transaction
local tx, err = db:begin()
if err then error(err) end

-- Perform multiple operations
local ok, err = tx:execute("UPDATE accounts SET balance = balance - ? WHERE id = ?", {100, 1})
if err then
    tx:rollback()
    error("Transfer failed: " .. err)
end

ok, err = tx:execute("UPDATE accounts SET balance = balance + ? WHERE id = ?", {100, 2})
if err then
    tx:rollback()
    error("Transfer failed: " .. err)
end

-- Commit the transaction
ok, err = tx:commit()
if err then error("Commit failed: " .. err) end

-- Release the database when done
db:release()
```

### Prepared Statement Example

```lua
-- Get database connection
local db, err = sql.get("main_db")
if err then error(err) end

-- Prepare a statement
local stmt, err = db:prepare("INSERT INTO logs (timestamp, level, message) VALUES (?, ?, ?)")
if err then error(err) end

-- Execute it multiple times
local now = os.time()
local result, err = stmt:execute({now, "INFO", "Application started"})
if err then error(err) end

-- Later...
result, err = stmt:execute({now + 60, "ERROR", "Connection failed"})
if err then error(err) end

-- Release the database when done
db:release()
```

### Savepoint Example

```lua
-- Get database connection
local db, err = sql.get("main_db")
if err then error(err) end

-- Begin transaction
local tx, err = db:begin()
if err then error(err) end

-- First operation
local ok, err = tx:execute("INSERT INTO products (name, price) VALUES (?, ?)", {"Widget", 9.99})
if err then
    tx:rollback()
    error(err)
end

-- Create savepoint
ok, err = tx:savepoint("product_added")
if err then
    tx:rollback()
    error(err)
end

-- Second operation
ok, err = tx:execute("UPDATE inventory SET count = count - 1 WHERE product_id = ?", {last_id})
if err then
    -- Roll back to savepoint instead of whole transaction
    tx:rollback_to("product_added")
    
    -- Still commit the product addition
    tx:commit()
    print("Product added but inventory not updated")
else
    -- Everything succeeded
    tx:commit()
    print("Product added and inventory updated")
end

-- Release the database when done
db:release()
```