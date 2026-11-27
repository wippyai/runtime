<!-- SPDX-License-Identifier: MPL-2.0 -->

# json

JSON encoding and decoding with schema validation. Encoding, deterministic.

## Loading

```lua
local json = require("json")
```

## Functions

### encode(value: any) → string

Encodes a Lua value into a JSON string.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| value | any | yes | - | Any Lua value (nil, boolean, number, string, table) |

**Returns:** `string` - JSON string representation (nil encodes as "null")

**Errors (structured):**

| Condition | Kind | Retryable | Notes |
|-----------|------|-----------|-------|
| recursively nested table | errors.INTERNAL | no | Table contains itself |
| sparse array | errors.INTERNAL | no | A gap before a key kept past the array part (e.g., {[1]="a", [1000]="c"}); the message names the key and the array length; see encoding rules |
| mixed-key table | errors.INTERNAL | no | Table has both numeric and non-numeric keys |
| max depth exceeded | errors.INTERNAL | no | Exceeds 128 levels of nesting |

**Encoding rules:**
- `nil` → `"null"`
- `json.null` → `"null"`, also as an object value or array element (`{a = json.null}` → `{"a":null}`)
- A `json.raw` value → its JSON text as stored, with member order kept
- Booleans → `"true"` or `"false"`
- Numbers → JSON numbers (Inf/NaN → `"null"`)
- Strings → JSON strings with escaping
- Tables with sequential numeric keys (1-based) → JSON arrays
- `nil` elements inside an array → `null` (`{1, nil, 3}` → `[1,null,3]`); trailing `nil` elements are omitted
- Integer keys stored outside the table's array part must continue the array without a gap, otherwise encoding fails with a sparse-array error. go-lua keeps a key in the array part when it lands at most 32 slots past its end: `{[1]="a", [34]="c"}` encodes with 32 `null`s, `{[1]="a", [35]="c"}` fails, and the outcome can depend on insertion order (`{[1]="a", [20]="b", [40]="c"}` encodes, `{[1]="a", [40]="c", [20]="b"}` fails)
- Every positive integer key is an array position, whatever its size: `{[1730000000]="ts"}` is a sparse array, and `{[1730000000]="ts", a=1}` is a mixed-key table
- Zero, negative and fractional numeric keys are object keys: `{[0]="z", [-1]="m"}` → `{"-1":"m","0":"z"}`
- Tables with string keys → JSON objects
- Empty tables → `"[]"`

### decode(str: string) → any, error

Decodes a JSON string into a Lua value.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| str | string | yes | - | JSON string to decode |

**Returns:**
- Success: Lua value (nil for "null")
- Error: `nil, error` - error is structured

**Errors (structured):**

| Condition | Kind | Retryable | Notes |
|-----------|------|-----------|-------|
| input not string | errors.INVALID | no | Non-string argument |
| empty string | errors.INVALID | no | Empty string is not valid JSON |
| invalid JSON | errors.INTERNAL | no | Malformed JSON syntax |

**Decoding rules:**
- `"null"` → `nil`
- JSON booleans → Lua booleans
- JSON numbers → Lua numbers
- JSON strings → Lua strings
- JSON arrays → 1-indexed Lua tables
- `null` array elements → `nil`; later elements keep their indexes, so `json.encode` writes them back as `null`
- Trailing `null` array elements and `null` object values are lost on a round trip: `["a",null]` → `["a"]`, `[null,null]` → `[]`, `{"a":null}` → `{}`; `decode_with_nulls` keeps them
- JSON objects → Lua tables with string keys

### decode_with_nulls(str: string) → any, error

Decodes like `decode`, except that every JSON `null` decodes to `json.null`.
Null object members stay present and null array elements keep their positions,
so the result encodes back to the same JSON value:

```lua
local args = json.decode_with_nulls('{"a":null,"b":[1,null]}')
assert(args.a == json.null)
assert(#args.b == 2 and args.b[2] == json.null)
json.encode(args) -- {"a":null,"b":[1,null]}
```

Errors are those of `decode`. `json.null` is a truthy value, so code reading
the result compares against `json.null` to tell an explicit null from a value.

### null

A single userdata value that stands for JSON `null`. Unlike `nil`, it can be
stored in a table, so it marks a key that is present with a null value. Every
Lua process shares the one value; compare with `==`. `encode` writes it as
`null`, `validate` reads it as `null`, and it passes between Lua processes
(function arguments and results, messages) unchanged. Conversion to Go data
gives `nil` and keeps the map key; Go data converted to Lua has no null members,
so a value that passes through Go data loses them.

### raw(text: string) → raw, error

Wraps JSON text in a value that `encode` writes as that text. Object members
keep the order they have in `text`; Lua tables have no key order and `encode`
writes their keys sorted, so `raw` is how ordered JSON (such as a JSON Schema
whose property order matters) is embedded in an encoded document:

```lua
local schema = json.raw('{"type":"object","properties":{"query":{},"limit":{}}}')
json.encode({name = "search", parameters = schema})
-- {"name":"search","parameters":{"type":"object","properties":{"query":{},"limit":{}}}}
```

`text` must be exactly one JSON value with unique member names, valid UTF-8
and numbers in float64 range; otherwise `raw` returns `nil` and an
`errors.INVALID` error. The value is stored without insignificant whitespace.
A raw value is opaque to Lua code: decode the text to inspect it. It passes
between Lua processes unchanged; `validate` and conversion to Go data read it
as the JSON value it holds.

### decode(str: string, target: type) → target, error

Decodes JSON and validates the result against a Lua type value. After checking
the error, the result has the requested type.

```lua
type User = {id: string, nickname: string?}
local user, err = json.decode('{"id":"u1","nickname":null}', User)
if err then return nil, err end
local id: string = user.id
```

Success always returns a non-nil value. Top-level JSON `null` returns
`nil, error` even for a nullable target; nullable fields and array elements
remain valid. The one-argument form still decodes `null` to nil.

Invalid type arguments, malformed JSON, type mismatches, and top-level `null`
return a structured `errors.INVALID` error with `retryable = false`.

### validate(schema: table | string | raw, data: any) → boolean, error

Validates a Lua value against a JSON Schema.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| schema | table \| string \| raw | yes | - | JSON Schema as Lua table, JSON string or `json.raw` value |
| data | any | yes | - | Lua value to validate |

**Returns:**
- Success: `true` - data is valid
- Error: `false, error` - error is structured

**Errors (structured):**

| Condition | Kind | Retryable | details.code | Notes |
|-----------|------|-----------|--------------|-------|
| schema is nil | errors.INVALID | no | - | Schema parameter required |
| data is nil | errors.INVALID | no | - | Data parameter required |
| schema invalid | errors.INVALID | no | `schema` | Schema not table, string or raw |
| schema compile error | errors.INVALID | no | `schema` | Invalid JSON Schema or unresolvable `$ref` |
| validation failed | errors.INVALID | no | `validation` | Data doesn't match schema |

**Schema caching:** Schemas are cached internally by SHA256 hash (cache size: 100).

### validate_string(schema: table | string | raw, json_str: string) → boolean, error

Validates a JSON string against a JSON Schema without decoding it first.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| schema | table \| string \| raw | yes | - | JSON Schema as Lua table, JSON string or `json.raw` value |
| json_str | string | yes | - | JSON string to validate |

**Returns:**
- Success: `true` - JSON string is valid
- Error: `false, error` - error is structured

**Errors (structured):**

| Condition | Kind | Retryable | details.code | Notes |
|-----------|------|-----------|--------------|-------|
| schema is nil | errors.INVALID | no | - | Schema parameter required |
| data not string | errors.INVALID | no | - | Second parameter must be string |
| schema invalid | errors.INVALID | no | `schema` | Schema not table, string or raw |
| schema compile error | errors.INVALID | no | `schema` | Invalid JSON Schema or unresolvable `$ref` |
| validation failed | errors.INVALID | no | `validation` | JSON doesn't match schema |

**Validation failure format:** the message is `validation failed: <instance path>: <keyword>: <text>` for the
most specific violated rule. `if`/`not`/`contains` sub-results are conditions, not violations; `$ref`, `allOf`,
`anyOf`, `oneOf`, `properties` and `items` wrappers are descended to their leaf rules. For `required` the instance
path is the missing property (`/a/b`, keyword `required`); `dependentRequired` likewise reports one leaf per missing property; for `additionalProperties` it is the extra property; for `propertyNames` it is the offending property name with the failing sub-schema keyword.
`err:details()` returns `{code = "validation", errors = [{instance_path, keyword, message, schema_path}], total = n}`, ordered most
specific first (`required`, `additionalProperties`/`unevaluatedProperties`, `enum`/`const`, `type`, others; then
deepest path first) and holding at most 50 entries; `total` is the full leaf count. The root path is `/`.

## Errors

This module returns structured errors. Check kind with `errors.*` constants:

```lua
local result, err = json.decode("invalid")
if err then
    if err:kind() == errors.INVALID then
        -- bad input
    elseif err:kind() == errors.INTERNAL then
        -- encoding/decoding failed
    end
    print(err:message())
    print(err:retryable())  -- always false for this module
end
```

**Possible kinds:** `errors.INVALID`, `errors.INTERNAL`

## Example

```lua
local json = require("json")

-- Encode a table to JSON
local data = {name = "John", age = 30, active = true}
local encoded = json.encode(data)
-- Result: {"name":"John","age":30,"active":true} (order may vary)

-- Decode JSON to Lua table
local decoded, err = json.decode('{"name":"Jane","items":[1,2,3]}')
if err then
    error(err:message())
end
print(decoded.name)     -- "Jane"
print(decoded.items[1]) -- 1

-- Validate data against schema
local schema = {
    type = "object",
    properties = {
        name = {type = "string"},
        age = {type = "number", minimum = 0}
    },
    required = {"name"}
}

local valid, err2 = json.validate(schema, {name = "John", age = 30})
if not valid then
    error(err2:message())
end

-- Validate JSON string directly
local valid2, err3 = json.validate_string(schema, '{"name":"Jane","age":25}')
if not valid2 then
    error(err3:message())
end

-- Round-trip encode/decode
local original = {items = {1, 2, 3}, meta = {ok = true}}
local json_str = json.encode(original)
local restored = json.decode(json_str)
```
