<!-- SPDX-License-Identifier: MPL-2.0 -->

# sql

SQL database operations with query builder and transaction support. Storage, io, nondeterministic.

## Loading

```lua
local sql = require("sql")
```

## Constants

```lua
-- Database types
sql.type.POSTGRES    -- "postgres"
sql.type.MYSQL       -- "mysql"
sql.type.SQLITE      -- "sqlite"
sql.type.MSSQL       -- "mssql"
sql.type.ORACLE      -- "oracle"
sql.type.UNKNOWN     -- "unknown"

-- Transaction isolation levels
sql.isolation.DEFAULT           -- "default"
sql.isolation.READ_UNCOMMITTED  -- "read_uncommitted"
sql.isolation.READ_COMMITTED    -- "read_committed"
sql.isolation.WRITE_COMMITTED   -- "write_committed"
sql.isolation.REPEATABLE_READ   -- "repeatable_read"
sql.isolation.SERIALIZABLE      -- "serializable"

-- NULL marker
sql.NULL  -- represents SQL NULL value

-- Builder placeholder formats
sql.builder.question            -- ? placeholder (default)
sql.builder.dollar              -- $1, $2, ... placeholder
sql.builder.at                  -- @p1, @p2, ... placeholder
sql.builder.colon               -- :1, :2, ... placeholder
sql.builder.default_placeholder -- alias for question
```

## Functions

### get(id: string) → DB, error

Acquires database connection from resource registry.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| id | string | yes | - | Resource ID (e.g., "app.db:main") |

**Returns:**
- Success: `DB` - database handle
- Error: `nil, error` - structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| id empty | errors.INVALID | no |
| permission denied | errors.PERMISSION_DENIED | no |
| resource not found | errors.NOT_FOUND | no |
| resource not database | errors.INVALID | no |

**Yields:** until resource acquired

## sql.as

Type coercion functions for explicit SQL type mapping.

### as.int(value: number) → userdata

Coerces value to SQL integer type.

**Returns:** userdata representing typed integer value

### as.float(value: number) → userdata

Coerces value to SQL float type.

**Returns:** userdata representing typed float value

### as.text(value: any) → userdata

Coerces value to SQL text type.

**Returns:** userdata representing typed text value

### as.binary(value: string) → userdata

Coerces value to SQL binary type.

**Returns:** userdata representing typed binary value

### as.null() → userdata

Returns SQL NULL marker (alternative to sql.NULL constant).

**Returns:** userdata representing SQL NULL

## sql.builder

SQL query builder with fluent interface.

### builder.select(columns...: string) → SelectBuilder

Creates SELECT query builder.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| columns | ...string | no | - | Column names (can add more with :columns()) |

**Returns:** `SelectBuilder` - builder instance

### builder.insert(table?: string) → InsertBuilder

Creates INSERT query builder.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| table | string | no | "" | Table name (can set with :into()) |

**Returns:** `InsertBuilder` - builder instance

### builder.update(table?: string) → UpdateBuilder

Creates UPDATE query builder.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| table | string | no | "" | Table name (can set with :table()) |

**Returns:** `UpdateBuilder` - builder instance

### builder.delete(table?: string) → DeleteBuilder

Creates DELETE query builder.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| table | string | no | "" | Table name (can set with :from()) |

**Returns:** `DeleteBuilder` - builder instance

### builder.expr(sql: string, args...: any) → Sqlizer

Creates raw SQL expression for use in where/having clauses.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| sql | string | yes | - | SQL expression with ? placeholders |
| args | ...any | no | - | Bind arguments |

**Returns:** `Sqlizer` - SQL expression

```lua
local expr = sql.builder.expr("score BETWEEN ? AND ?", 80, 90)
```

### builder.eq(map: table) → Sqlizer

Creates equality condition from table.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| map | table | yes | - | {column = value} pairs |

**Returns:** `Sqlizer` - equality condition

```lua
local cond = sql.builder.eq({active = 1, status = "open"})
```

### builder.not_eq(map: table) → Sqlizer

Creates inequality condition from table.

**Returns:** `Sqlizer` - inequality condition

### builder.lt(map: table) → Sqlizer

Creates less-than condition from table.

**Returns:** `Sqlizer` - less-than condition

### builder.lte(map: table) → Sqlizer

Creates less-than-or-equal condition from table.

**Returns:** `Sqlizer` - less-than-or-equal condition

### builder.gt(map: table) → Sqlizer

Creates greater-than condition from table.

**Returns:** `Sqlizer` - greater-than condition

### builder.gte(map: table) → Sqlizer

Creates greater-than-or-equal condition from table.

**Returns:** `Sqlizer` - greater-than-or-equal condition

### builder.like(map: table) → Sqlizer

Creates LIKE condition from table.

**Returns:** `Sqlizer` - LIKE condition

```lua
local cond = sql.builder.like({name = "john%"})
```

### builder.not_like(map: table) → Sqlizer

Creates NOT LIKE condition from table.

**Returns:** `Sqlizer` - NOT LIKE condition

### builder.and_(conditions: table) → Sqlizer

Combines multiple conditions with AND.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| conditions | table | yes | - | Array of Sqlizer or table conditions |

**Returns:** `Sqlizer` - AND condition

```lua
local cond = sql.builder.and_({
    sql.builder.eq({active = 1}),
    sql.builder.gt({score = 80})
})
```

### builder.or_(conditions: table) → Sqlizer

Combines multiple conditions with OR.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| conditions | table | yes | - | Array of Sqlizer or table conditions |

**Returns:** `Sqlizer` - OR condition

## Types

### DB

Database connection handle. Returned by `sql.get()`. Auto-released on process cleanup.

| Method | Signature | Returns | Notes |
|--------|-----------|---------|-------|
| type | () | string, error | Database type (sql.type.*) |
| query | (sql: string, params?: table) | rows: table[], error | Execute SELECT query |
| execute | (sql: string, params?: table) | result: table, error | Execute INSERT/UPDATE/DELETE |
| prepare | (sql: string) | Statement, error | Create prepared statement |
| begin | (options?: table) | Transaction, error | Begin transaction |
| release | () | boolean, error | Release database resource |
| stats | () | table, error | Get connection pool statistics |

**Methods yield:** query, execute, prepare, begin

#### db:type() → string, error

Returns database type constant.

**Returns:**
- Success: `string` - one of sql.type.* constants
- Error: `nil, error`

**Yields:** no

#### db:query(sql: string, params?: table) → table[], error

Executes SELECT query and returns rows.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| sql | string | yes | - | SQL query with ? placeholders |
| params | table | no | nil | Array of bind parameters |

**Returns:**
- Success: `table[]` - array of row tables with column names as keys
- Error: `nil, error` - structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| invalid params type | errors.INVALID | no |
| SQL syntax error | errors.INVALID | no |
| query execution error | varies | varies |

**Yields:** until query completes

```lua
local rows, err = db:query("SELECT id, name FROM users WHERE active = ?", {1})
if err then error(err) end
for _, row in ipairs(rows) do
    print(row.id, row.name)  -- access by column name
end
```

#### db:execute(sql: string, params?: table) → table, error

Executes INSERT/UPDATE/DELETE query.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| sql | string | yes | - | SQL statement with ? placeholders |
| params | table | no | nil | Array of bind parameters |

**Returns:**
- Success: `table` - {last_insert_id: integer, rows_affected: integer}
- Error: `nil, error` - structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| invalid params type | errors.INVALID | no |
| SQL syntax error | errors.INVALID | no |
| execution error | varies | varies |

**Yields:** until execution completes

```lua
local result, err = db:execute("INSERT INTO users (name) VALUES (?)", {"alice"})
if err then error(err) end
print("Inserted ID:", result.last_insert_id)
print("Rows affected:", result.rows_affected)
```

#### db:prepare(sql: string) → Statement, error

Creates prepared statement for repeated execution.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| sql | string | yes | - | SQL with ? placeholders |

**Returns:**
- Success: `Statement` - prepared statement
- Error: `nil, error` - structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| SQL syntax error | errors.INVALID | no |
| prepare error | varies | varies |

**Yields:** until prepared

```lua
local stmt, err = db:prepare("SELECT * FROM users WHERE id = ?")
if err then error(err) end
-- use stmt:query() or stmt:execute() multiple times
stmt:close()
```

#### db:begin(options?: table) → Transaction, error

Begins database transaction.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| options | table | no | nil | Transaction options |

**options fields:**

| Field | Type | Default | Notes |
|-------|------|---------|-------|
| isolation | string | sql.isolation.DEFAULT | Isolation level from sql.isolation.* |
| read_only | boolean | false | Read-only transaction flag |

**Returns:**
- Success: `Transaction` - transaction handle
- Error: `nil, error` - structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| invalid isolation level | errors.INVALID | no |
| begin error | varies | varies |

**Yields:** until transaction begins

```lua
local tx, err = db:begin({
    isolation = sql.isolation.SERIALIZABLE,
    read_only = false
})
if err then error(err) end
-- use tx:query(), tx:execute(), etc.
tx:commit()
```

#### db:release() → boolean, error

Releases database resource back to pool. Safe to call multiple times.

**Returns:**
- Success: `true, nil`
- Error: `nil, error`

**Yields:** no

#### db:stats() → table, error

Returns connection pool statistics.

**Returns:**
- Success: `table` - statistics object
- Error: `nil, error`

**Statistics fields:**

| Field | Type | Notes |
|-------|------|-------|
| max_open_connections | number | Max allowed open connections |
| open_connections | number | Current open connections |
| in_use | number | Connections currently in use |
| idle | number | Idle connections in pool |
| wait_count | number | Total connection wait count |
| wait_duration | string | Total wait duration |
| max_idle_closed | number | Connections closed due to max idle |
| max_idle_time_closed | number | Connections closed due to idle timeout |
| max_lifetime_closed | number | Connections closed due to max lifetime |

**Yields:** no

### Statement

Prepared statement. Auto-closed on process cleanup.

| Method | Signature | Returns | Notes |
|--------|-----------|---------|-------|
| query | (params?: table) | rows: table[], error | Execute as SELECT |
| execute | (params?: table) | result: table, error | Execute as INSERT/UPDATE/DELETE |
| close | () | boolean, error | Close statement |

**Methods yield:** query, execute, close

#### stmt:query(params?: table) → table[], error

Executes prepared statement as SELECT.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| params | table | no | nil | Array of bind parameters |

**Returns:**
- Success: `table[]` - array of row tables
- Error: `nil, error` - structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| statement closed | errors.INVALID | no |
| invalid params type | errors.INVALID | no |
| query error | varies | varies |

**Yields:** until query completes

#### stmt:execute(params?: table) → table, error

Executes prepared statement as INSERT/UPDATE/DELETE.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| params | table | no | nil | Array of bind parameters |

**Returns:**
- Success: `table` - {last_insert_id: integer, rows_affected: integer}
- Error: `nil, error` - structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| statement closed | errors.INVALID | no |
| invalid params type | errors.INVALID | no |
| execution error | varies | varies |

**Yields:** until execution completes

#### stmt:close() → boolean, error

Closes prepared statement. Safe to call multiple times.

**Returns:**
- Success: `true, nil`
- Error: `nil, error` - structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| already closed | errors.INVALID | no |
| close error | varies | varies |

**Yields:** until closed

### Transaction

Database transaction. Auto-rollback on process cleanup if not committed.

| Method | Signature | Returns | Notes |
|--------|-----------|---------|-------|
| db_type | () | string, error | Database type |
| query | (sql: string, params?: table) | rows: table[], error | Execute SELECT in transaction |
| execute | (sql: string, params?: table) | result: table, error | Execute INSERT/UPDATE/DELETE in transaction |
| prepare | (sql: string) | Statement, error | Prepare statement in transaction |
| commit | () | boolean, error | Commit transaction |
| rollback | () | boolean, error | Rollback transaction |
| savepoint | (name: string) | boolean, error | Create savepoint |
| rollback_to | (name: string) | boolean, error | Rollback to savepoint |
| release | (name: string) | boolean, error | Release savepoint |

**Methods yield:** query, execute, prepare, commit, rollback, savepoint, rollback_to, release

#### tx:db_type() → string, error

Returns database type constant.

**Returns:**
- Success: `string` - one of sql.type.* constants
- Error: `nil, error`

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| transaction not active | errors.INVALID | no |

**Yields:** no

#### tx:query(sql: string, params?: table) → table[], error

Executes SELECT query within transaction. Same as db:query() but within transaction scope.

**Yields:** until query completes

#### tx:execute(sql: string, params?: table) → table, error

Executes INSERT/UPDATE/DELETE within transaction. Same as db:execute() but within transaction scope.

**Yields:** until execution completes

#### tx:prepare(sql: string) → Statement, error

Creates prepared statement within transaction. Same as db:prepare() but within transaction scope.

**Yields:** until prepared

#### tx:commit() → boolean, error

Commits transaction. Transaction becomes inactive after commit.

**Returns:**
- Success: `true, nil`
- Error: `nil, error` - structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| transaction not active | errors.INVALID | no |
| commit error | varies | varies |

**Yields:** until committed

#### tx:rollback() → boolean, error

Rolls back transaction. Transaction becomes inactive after rollback.

**Returns:**
- Success: `true, nil`
- Error: `nil, error` - structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| transaction not active | errors.INVALID | no |
| rollback error | varies | varies |

**Yields:** until rolled back

#### tx:savepoint(name: string) → boolean, error

Creates named savepoint within transaction.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| name | string | yes | - | Savepoint name (alphanumeric and underscore only) |

**Returns:**
- Success: `true, nil`
- Error: `nil, error` - structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| transaction not active | errors.INVALID | no |
| name empty | errors.INVALID | no |
| invalid name chars | errors.INVALID | no |
| savepoint error | varies | varies |

**Yields:** until savepoint created

```lua
local tx, _ = db:begin()
tx:execute("INSERT INTO users (name) VALUES (?)", {"alice"})
tx:savepoint("sp1")
tx:execute("INSERT INTO users (name) VALUES (?)", {"bob"})
tx:rollback_to("sp1")  -- rolls back bob insert, keeps alice
tx:commit()
```

#### tx:rollback_to(name: string) → boolean, error

Rolls back to named savepoint, undoing operations after savepoint creation.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| name | string | yes | - | Savepoint name |

**Returns:**
- Success: `true, nil`
- Error: `nil, error` - structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| transaction not active | errors.INVALID | no |
| name empty | errors.INVALID | no |
| invalid name chars | errors.INVALID | no |
| rollback error | varies | varies |

**Yields:** until rolled back

#### tx:release(name: string) → boolean, error

Releases savepoint, making it unavailable for rollback. Optional cleanup operation.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| name | string | yes | - | Savepoint name |

**Returns:**
- Success: `true, nil`
- Error: `nil, error` - structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| transaction not active | errors.INVALID | no |
| name empty | errors.INVALID | no |
| invalid name chars | errors.INVALID | no |
| release error | varies | varies |

**Yields:** until released

### SelectBuilder

Fluent SELECT query builder. All methods return new builder instance (immutable chaining).

| Method | Signature | Returns | Notes |
|--------|-----------|---------|-------|
| from | (table: string) | SelectBuilder | Set FROM clause |
| join | (join: string, args...: any) | SelectBuilder | Add JOIN clause |
| left_join | (join: string, args...: any) | SelectBuilder | Add LEFT JOIN clause |
| right_join | (join: string, args...: any) | SelectBuilder | Add RIGHT JOIN clause |
| inner_join | (join: string, args...: any) | SelectBuilder | Add INNER JOIN clause |
| where | (condition: string\|table\|Sqlizer, args...: any) | SelectBuilder | Add WHERE condition |
| order_by | (columns...: string) | SelectBuilder | Add ORDER BY clause |
| group_by | (columns...: string) | SelectBuilder | Add GROUP BY clause |
| having | (condition: string\|table\|Sqlizer, args...: any) | SelectBuilder | Add HAVING condition |
| limit | (n: integer) | SelectBuilder | Set LIMIT |
| offset | (n: integer) | SelectBuilder | Set OFFSET |
| columns | (columns...: string) | SelectBuilder | Add columns to SELECT |
| distinct | () | SelectBuilder | Add DISTINCT modifier |
| suffix | (sql: string, args...: any) | SelectBuilder | Add SQL suffix |
| placeholder_format | (format: userdata) | SelectBuilder | Set placeholder format |
| to_sql | () | string, table | Generate SQL and args |
| run_with | (db: DB\|Transaction) | QueryExecutor | Create executor |

#### where/having condition formats

The `where()` and `having()` methods accept three formats:

1. **String with args**: `where("status = ? AND score > ?", "active", 80)`
2. **Table (equality)**: `where({status = "active", active = 1})`
3. **Sqlizer**: `where(sql.builder.gt({score = 80}))`

#### select:to_sql() → string, table

Generates SQL string and bind arguments.

**Returns:**
- Success: `string, table` - SQL string and args array
- Error: `nil, error` - structured error on invalid query

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| invalid query structure | errors.INVALID | no |

```lua
local query = sql.builder.select("id", "name")
    :from("users")
    :where({active = 1})
    :order_by("name ASC")
    :limit(10)

local sql_str, args = query:to_sql()
-- sql_str: "SELECT id, name FROM users WHERE active = ? ORDER BY name ASC LIMIT 10"
-- args: {1}
```

#### select:run_with(db: DB|Transaction) → QueryExecutor

Creates executor for query.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| db | DB\|Transaction | yes | - | Database or transaction handle |

**Returns:**
- Success: `QueryExecutor` - executor instance
- Error: `nil, error` - structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| invalid query structure | errors.INVALID | no |

```lua
local query = sql.builder.select("*")
    :from("users")
    :where({active = 1})

local executor = query:run_with(db)
local rows, err = executor:query()
```

### InsertBuilder

Fluent INSERT query builder. All methods return new builder instance.

| Method | Signature | Returns | Notes |
|--------|-----------|---------|-------|
| into | (table: string) | InsertBuilder | Set table name |
| columns | (columns...: string) | InsertBuilder | Set column names |
| values | (values...: any) | InsertBuilder | Add row values |
| set_map | (map: table) | InsertBuilder | Set columns and values from table |
| select | (query: SelectBuilder) | InsertBuilder | Insert from SELECT |
| prefix | (sql: string, args...: any) | InsertBuilder | Add SQL prefix |
| suffix | (sql: string, args...: any) | InsertBuilder | Add SQL suffix |
| options | (options...: string) | InsertBuilder | Add INSERT options |
| placeholder_format | (format: userdata) | InsertBuilder | Set placeholder format |
| to_sql | () | string, table | Generate SQL and args |
| run_with | (db: DB\|Transaction) | QueryExecutor | Create executor |

```lua
local insert = sql.builder.insert("users")
    :columns("name", "email", "active")
    :values("alice", "alice@example.com", 1)

local executor = insert:run_with(db)
local result, err = executor:exec()
-- result.last_insert_id, result.rows_affected
```

### UpdateBuilder

Fluent UPDATE query builder. All methods return new builder instance.

| Method | Signature | Returns | Notes |
|--------|-----------|---------|-------|
| table | (table: string) | UpdateBuilder | Set table name |
| set | (column: string, value: any) | UpdateBuilder | Set column value |
| set_map | (map: table) | UpdateBuilder | Set multiple columns from table |
| where | (condition: string\|table\|Sqlizer, args...: any) | UpdateBuilder | Add WHERE condition |
| order_by | (columns...: string) | UpdateBuilder | Add ORDER BY clause |
| limit | (n: integer) | UpdateBuilder | Set LIMIT |
| offset | (n: integer) | UpdateBuilder | Set OFFSET |
| suffix | (sql: string, args...: any) | UpdateBuilder | Add SQL suffix |
| from | (table: string) | UpdateBuilder | Add FROM clause |
| from_select | (query: SelectBuilder, alias: string) | UpdateBuilder | Update from SELECT |
| placeholder_format | (format: userdata) | UpdateBuilder | Set placeholder format |
| to_sql | () | string, table | Generate SQL and args |
| run_with | (db: DB\|Transaction) | QueryExecutor | Create executor |

```lua
local update = sql.builder.update("users")
    :set("status", "active")
    :set("updated_at", sql.builder.expr("NOW()"))
    :where({id = 123})

local executor = update:run_with(db)
local result, err = executor:exec()
```

### DeleteBuilder

Fluent DELETE query builder. All methods return new builder instance.

| Method | Signature | Returns | Notes |
|--------|-----------|---------|-------|
| from | (table: string) | DeleteBuilder | Set table name |
| where | (condition: string\|table\|Sqlizer, args...: any) | DeleteBuilder | Add WHERE condition |
| order_by | (columns...: string) | DeleteBuilder | Add ORDER BY clause |
| limit | (n: integer) | DeleteBuilder | Set LIMIT |
| offset | (n: integer) | DeleteBuilder | Set OFFSET |
| suffix | (sql: string, args...: any) | DeleteBuilder | Add SQL suffix |
| placeholder_format | (format: userdata) | DeleteBuilder | Set placeholder format |
| to_sql | () | string, table | Generate SQL and args |
| run_with | (db: DB\|Transaction) | QueryExecutor | Create executor |

```lua
local delete = sql.builder.delete("users")
    :where({active = 0})
    :limit(100)

local executor = delete:run_with(db)
local result, err = executor:exec()
```

### QueryExecutor

Executes builder-generated queries. Returned by builder:run_with().

| Method | Signature | Returns | Notes |
|--------|-----------|---------|-------|
| query | () | rows: table[], error | Execute as SELECT (yields) |
| exec | () | result: table, error | Execute as INSERT/UPDATE/DELETE (yields) |
| to_sql | () | string, table | Get SQL and args |

#### executor:query() → table[], error

Executes query and returns rows. Use for SELECT statements.

**Returns:**
- Success: `table[]` - array of row tables
- Error: `nil, error` - structured error

**Yields:** until query completes

#### executor:exec() → table, error

Executes query and returns result. Use for INSERT/UPDATE/DELETE statements.

**Returns:**
- Success: `table` - {last_insert_id: integer, rows_affected: integer}
- Error: `nil, error` - structured error

**Yields:** until execution completes

#### executor:to_sql() → string, table

Returns generated SQL and arguments without executing.

**Returns:** `string, table` - SQL string and args array

**Yields:** no

### Sqlizer

SQL expression wrapper. Created by builder.expr() and comparison functions.

| Method | Signature | Returns | Notes |
|--------|-----------|---------|-------|
| to_sql | () | string, table | Generate SQL fragment and args |

## Errors

This module returns structured errors. Check kind with `errors.*` constants:

```lua
local result, err = db:query("SELECT * FROM users")
if err then
    if err:kind() == errors.INVALID then
        -- invalid query or parameters
    elseif err:kind() == errors.PERMISSION_DENIED then
        -- access denied
    elseif err:kind() == errors.NOT_FOUND then
        -- resource not found
    end
    error(err)
end
```

**Possible kinds:** `errors.INVALID`, `errors.PERMISSION_DENIED`, `errors.NOT_FOUND`, `errors.INTERNAL`, `errors.UNAVAILABLE`

## Example

```lua
local sql = require("sql")

-- Get database connection
local db, err = sql.get("app.db:main")
if err then error(err) end

-- Check database type
local dbtype, _ = db:type()
print("Database type:", dbtype)

-- Direct query with parameters
local users, err = db:query("SELECT id, name FROM users WHERE active = ?", {1})
if err then error(err) end

for _, user in ipairs(users) do
    print(user.id, user.name)
end

-- Builder pattern for complex queries
local query = sql.builder.select("u.id", "u.name", "COUNT(o.id) as order_count")
    :from("users u")
    :left_join("orders o ON o.user_id = u.id")
    :where(sql.builder.and_({
        sql.builder.eq({["u.active"] = 1}),
        sql.builder.gte({["u.score"] = 80})
    }))
    :group_by("u.id", "u.name")
    :having(sql.builder.gt({["COUNT(o.id)"] = 0}))
    :order_by("order_count DESC")
    :limit(10)

local executor = query:run_with(db)
local results, err = executor:query()
if err then error(err) end

-- Transaction with savepoints
local tx, err = db:begin({isolation = sql.isolation.SERIALIZABLE})
if err then error(err) end

local _, err = tx:execute("INSERT INTO users (name) VALUES (?)", {"alice"})
if err then
    tx:rollback()
    error(err)
end

tx:savepoint("before_update")

local _, err = tx:execute("UPDATE users SET status = ? WHERE id = ?", {"active", 1})
if err then
    tx:rollback_to("before_update")  -- rollback to savepoint
else
    tx:release("before_update")  -- release savepoint
end

local ok, err = tx:commit()
if err then error(err) end

-- Prepared statements for repeated execution
local stmt, err = db:prepare("INSERT INTO logs (message, level) VALUES (?, ?)")
if err then error(err) end

for i = 1, 100 do
    local _, err = stmt:execute({"log message " .. i, "info"})
    if err then
        stmt:close()
        error(err)
    end
end

stmt:close()

-- NULL and typed values
local insert = sql.builder.insert("products")
    :columns("name", "price", "description", "data")
    :values(
        "Widget",
        sql.as.float(19.99),
        sql.NULL,  -- NULL description
        sql.as.binary("binary data")
    )

local executor = insert:run_with(db)
local result, err = executor:exec()
if err then error(err) end

print("Inserted ID:", result.last_insert_id)

-- Release database
db:release()
```
