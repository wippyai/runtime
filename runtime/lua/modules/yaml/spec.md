# Lua YAML Module Specification

## Overview

The `yaml` module provides functions for encoding Lua tables into YAML (YAML Ain't Markup Language) strings and decoding
YAML strings into Lua tables. It handles nested structures and preserves multiline strings using the literal style for
better readability. The module also supports field ordering for controlling the structure of output YAML, with an option
to alphabetically sort unordered fields.

## Module Interface

### Module Loading

```lua
local yaml = require("yaml")
```

### Global Functions

#### yaml.encode(value: table[, field_order: table[, sort_unordered: boolean]])

Encodes a Lua table into a YAML string.

Parameters:

- `value`: The Lua table to encode.
- `field_order` (optional): A table containing field names in the desired order. Fields will be arranged according to
  their position in this table. Fields not in the table will appear after the ordered fields.
- `sort_unordered` (optional): A boolean value that determines if fields not in `field_order` should be sorted
  alphabetically. Default is `false`, which preserves the original order of unordered fields. When set to `true`,
  all fields not explicitly ordered by `field_order` will be sorted A-Z for stable output.

Returns:

- `encoded`: The YAML string representation of the table (or nil on error).
- `error`: Error message string (or nil on success).

#### yaml.decode(str: string)

Decodes a YAML string into a Lua table.

Parameters:

- `str`: The YAML string to decode.

Returns:

- `decoded`: The Lua table represented by the YAML string (or nil on error).
- `error`: Error message string (or nil on success).

## Error Handling

The module functions may return errors in the following cases:

1. **Missing Input:** If no input is provided to `encode` or `decode`.

    ```lua
    local encoded, err = yaml.encode() -- encoded: nil, err: "missing input table"
    local decoded, err = yaml.decode() -- decoded: nil, err: "missing input YAML string"
    ```

2. **Invalid Input Type (Encoding):** If the input to `encode` is not a table.

    ```lua
    local encoded, err = yaml.encode("not a table") -- encoded: nil, err: "first argument must be a table"
    ```

3. **Invalid YAML String (Decoding):** If the input to `decode` is not a valid YAML string.

    ```lua
    local decoded, err = yaml.decode("this is not valid yaml: :") -- decoded: nil, err: specific YAML parsing error message
    ```

4. **Empty String (Decoding):** If the input to `decode` is an empty string.

    ```lua
    local decoded, err = yaml.decode("") -- decoded: nil, err: "first argument must be a string"
    ```

## Behavior

### Encoding

1. **Tables:**
   - Tables are encoded as YAML maps or sequences based on their structure.
   - Nested tables are supported and properly encoded.

2. **Multiline Strings:**
   - Strings containing newlines are encoded using the YAML literal style (pipe character `|`).
   - This preserves the formatting of multiline strings for better readability.

3. **Data Types:**
   - Lua numbers are encoded as YAML numbers.
   - Lua strings are encoded as YAML strings, with multiline detection.
   - Lua booleans are encoded as YAML boolean values.
   - Lua nil values are encoded as YAML null.

4. **Field Ordering:**
   - When the optional `field_order` parameter is provided, fields in the output YAML will be ordered according to
     their position in this table.
   - Fields not specified in the ordering will appear after the ordered fields.
   - The ordering applies to all levels of the YAML structure.
   - When the optional `sort_unordered` parameter is set to `true`, fields not in `field_order` will be sorted
     alphabetically for stable output. This applies to all levels of the YAML structure.

### Decoding

1. **YAML to Lua:**
   - YAML maps are decoded as Lua tables with string keys.
   - YAML sequences are decoded as Lua tables with numeric indices.
   - YAML scalars are decoded as the appropriate Lua types (string, number, boolean, etc.).
   - YAML null values are decoded as Lua nil.

2. **Multiline Strings:**
   - YAML strings in literal style (pipe character `|`) or folded style (greater-than character `>`) are decoded as
     proper multiline strings in Lua.

## Field Ordering and Alphabetical Sorting

When encoding data to YAML, you can control the order of fields in the output:

```lua
local data = {
    version = "1.0",
    namespace = "app.agent.exec",
    meta = { depends_on = { "ns:system", "ns:app.tools.exec" } },
    entries = { { name = "exec_agent", kind = "registry.entry" } }
}

-- Define desired field order
local field_order = {
    "version",
    "namespace",
    "meta",
    "depends_on",
    "entries",
    "name",
    "kind"
}

-- Encode with field ordering only
local yaml_string, err = yaml.encode(data, field_order)

-- Encode with field ordering AND alphabetical sorting of unordered fields
local stable_yaml, err = yaml.encode(data, field_order, true)
```

Fields will be ordered based on their position in the `field_order` table. When the third parameter is `false` or not provided, fields not specified in `field_order` will appear after the ordered fields in their original order. When the third parameter is `true`, fields not specified in `field_order` will be sorted alphabetically for stable output.

## Thread Safety

- The `yaml` module is designed to be thread-safe in common cases.
- Encoding and decoding operations do not share any mutable state.
- However, if you are modifying a table while encoding it from another thread, the behavior is undefined.

## Best Practices

1. **Always check for errors:** Check the returned `error` value from both `encode` and `decode`.
2. **Validate input:** Ensure the input to `encode` is a valid Lua table, and the input to `decode` is a valid YAML
   string.
3. **Use multiline strings:** Take advantage of the automatic literal style formatting for multiline strings.
4. **Structure your data:** Organize your data in a logical structure that maps well to YAML's hierarchical format.
5. **Use field ordering for configurations:** Use the field ordering feature to ensure configuration files have a
   consistent, readable structure.
6. **Enable alphabetical sorting for stable output:** When generating YAML that will be compared or version-controlled,
   enable the `sort_unordered` option to ensure stable, deterministic output.

## Example Usage

```lua
local yaml = require("yaml")

-- Encode a Lua table with alphabetical sorting of unordered fields
local myTable = {
  name = "Configuration",
  version = 1.5,
  enabled = true,
  settings = {
    timeout = 30,
    retry = 3,
    endpoints = {"api.example.com", "backup.example.com"}
  },
  multilineText = [[
This is a multiline
text that will be formatted
using the YAML literal style.
]]
}

-- Specify fields that should appear first, in this exact order
local fieldOrder = {"name", "version"}

-- Get stable output with alphabetical sorting of remaining fields
local encoded, err = yaml.encode(myTable, fieldOrder, true)
if err then
  print("Encoding error:", err)
else
  print("Encoded YAML:")
  print(encoded)
  -- Output will have name and version first, then all other fields in A-Z order
  -- Multiline text will use the | literal style
end

-- Control field ordering in the output
local configData = {
  name = "Service Config",
  port = 8080,
  version = "2.1.0",
  enabled = true
}

-- Specify the desired field order
local fieldOrder = {"version", "name", "enabled", "port"}
local ordered, err = yaml.encode(configData, fieldOrder)
if not err then
  print("Ordered YAML:")
  print(ordered)
  -- Output will show fields in the order: version, name, enabled, port
end

-- Decode a YAML string
local yamlString = [[
name: UserProfile
user:
  username: john_doe
  email: john@example.com
  active: true
preferences:
  theme: dark
  notifications: true
bio: |
  Software developer with
  experience in multiple
  programming languages.
]]

local decoded, err = yaml.decode(yamlString)
if err then
  print("Decoding error:", err)
else
  print("Decoded name:", decoded.name)  -- Output: Decoded name: UserProfile
  print("Decoded username:", decoded.user.username)  -- Output: Decoded username: john_doe
  print("Decoded bio:", decoded.bio)  -- Will print the multiline bio with line breaks
end

-- Round trip with stable output
local original = {
  config = {
    server = "localhost",
    port = 8080,
    credentials = {
      username = "admin",
      password = "secret"
    }
  },
  documentation = [[
  This is a sample
  configuration for
  the application.
  ]]
}

-- Field order to ensure credentials always come last
local fieldOrder = {"server", "port"}
local encoded = yaml.encode(original, fieldOrder, true)  -- true for alphabetical sorting of unordered fields
local decoded, err = yaml.decode(encoded)
if err then
  print("Round trip error:", err)
else
  print("Original server:", original.config.server)
  print("Decoded server:", decoded.config.server)
  print("Documentation preserved:", decoded.documentation:find("sample") ~= nil)
end
```