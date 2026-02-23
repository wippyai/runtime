<!-- SPDX-License-Identifier: MPL-2.0 -->

# expr

Expression language evaluation using expr-lang syntax. Deterministic.

## Loading

```lua
local expr = require("expr")
```

## Functions

### eval(expression: string, env?: table) → any, error

Evaluates an expression string and returns the result. Uses internal LRU cache for compiled expressions.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| expression | string | yes | - | expr-lang syntax expression |
| env | table | no | nil | Variable environment for expression |

**Returns:**
- Success: `any` - Result of expression evaluation (integer, number, boolean, string, nil, or table)
- Error: `nil, error` - Structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| expression is empty | errors.INVALID | no |
| expression syntax invalid | errors.INTERNAL | no |
| expression evaluation fails | errors.INTERNAL | no |
| result conversion fails | errors.INTERNAL | no |

**Notes:**
- Compiled expressions are cached (default capacity: 1000)
- Cache key is the expression string only (env is not part of cache key)
- Supports expr-lang operators: `+`, `-`, `*`, `/`, `%`, `&&`, `||`, `!`, `>`, `<`, `>=`, `<=`, `==`, `!=`
- Supports ternary operator: `condition ? value_if_true : value_if_false`
- Supports builtin functions: `max()`, `min()`, `len()`, and others from expr-lang
- Supports arrays: `[1, 2, 3]`
- Supports string concatenation with `+`: `"hello" + " " + "world"`

### compile(expression: string, env?: table) → Program, error

Compiles an expression into a reusable Program object.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| expression | string | yes | - | expr-lang syntax expression |
| env | table | no | nil | Type hint environment for compilation |

**Returns:**
- Success: `Program` - Compiled program object
- Error: `nil, error` - Structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| expression is empty | errors.INVALID | no |
| expression syntax invalid | errors.INTERNAL | no |

**Notes:**
- env parameter is optional type hint for compilation, not data for evaluation
- Use `program:run(env)` to execute with actual data
- Compiled programs can be reused with different environments

## Types

### Program

Returned by `expr.compile()`. Represents a compiled expression.

| Method | Signature | Returns | Notes |
|--------|-----------|---------|-------|
| run | (env?: table) → any, error | Evaluation result or nil + error | env provides variables for expression |

#### program:run(env?: table) → any, error

Executes compiled expression with provided environment.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| env | table | no | nil | Variable environment for expression |

**Returns:**
- Success: `any` - Result of expression evaluation
- Error: `nil, error` - Structured error

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| expression evaluation fails | errors.INTERNAL | no |
| missing required variable | errors.INTERNAL | no |
| result conversion fails | errors.INTERNAL | no |

## Errors

This module returns structured errors. Check kind with `errors.*` constants:

```lua
local result, err = expr.eval("x + y", {x = 10})
if err then
    if err:kind() == errors.INVALID then
        -- empty or invalid expression syntax
    elseif err:kind() == errors.INTERNAL then
        -- compilation, evaluation, or conversion error
    end
end
```

**Possible kinds:** `errors.INVALID`, `errors.INTERNAL`

## Example

```lua
local expr = require("expr")

-- simple evaluation
local result, err = expr.eval("1 + 2")
if err then error(err) end
print(result)  -- 3

-- with environment
result, err = expr.eval("x + y", {x = 10, y = 20})
if err then error(err) end
print(result)  -- 30

-- boolean and comparison
result, err = expr.eval("x > 5", {x = 10})
if err then error(err) end
print(result)  -- true

-- ternary operator
result, err = expr.eval('x > 0 ? "positive" : "negative"', {x = 5})
if err then error(err) end
print(result)  -- "positive"

-- builtin functions
result, err = expr.eval("max(1, 5, 3)")
if err then error(err) end
print(result)  -- 5

-- compile and reuse
local program, err = expr.compile("a * b")
if err then error(err) end

result, err = program:run({a = 5, b = 6})
if err then error(err) end
print(result)  -- 30

result, err = program:run({a = 10, b = 3})
if err then error(err) end
print(result)  -- 30
```
