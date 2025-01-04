local treesitter = require("treesitter")

local code = [[
local MyLib = {}
MyLib.__index = MyLib

function MyLib:init()
    self.value = 0
end

function MyLib:process(data, options)
    self.value = data
    self.options = options
end

function MyLib:calculate(numbers, callback)
    local result = 0
    for _, n in ipairs(numbers) do
        if callback(n) then
            result = result + n
        end
    end
    return result
end

function MyLib.create(config)
    local instance = setmetatable({}, MyLib)
    instance.config = config
    return instance
end

function MyLib:append(prefix, ...)
    local args = {...}
    for _, v in ipairs(args) do
        self.value = self.value .. prefix .. tostring(v)
    end
end

function MyLib:configure(options, strict)
    self.strict = strict or false
    for k, v in pairs(options) do
        self[k] = v
    end
end

return MyLib
]]

local parser = treesitter.parser()
assert(parser:set_language("lua"), "Failed to set Lua language")

local tree = parser:parse(code)
local root = tree:root_node()

local method_query, err = treesitter.query("lua", [[
(function_declaration
  name: (method_index_expression
    table: (_) @table_name
    method: (identifier) @method_name)
  parameters: (parameters) @params) @function

(function_declaration
  name: (dot_index_expression
    table: (_) @table_name
    field: (identifier) @method_name)
  parameters: (parameters) @params) @static_function
]])
assert(err == nil, "Failed to create query")

local matches, err = method_query:matches(root, code)
assert(err == nil, "Failed to run matches")

local instance_methods = {}
local static_methods = {}

for _, match in ipairs(matches) do
    local method_info = { name = "", table = "", params = {} }

    for _, capture in ipairs(match.captures) do
        if capture.name == "method_name" then
            method_info.name = capture.node:text(code)
        elseif capture.name == "table_name" then
            method_info.table = capture.node:text(code)
        elseif capture.name == "params" then
            method_info.params = capture.node:text(code)
        end
    end

    if #match.captures > 0 and match.captures[1].name == "function" then
        table.insert(instance_methods, method_info)
    else
        table.insert(static_methods, method_info)
    end
end

-- Helper to find method by name
local function find_method(methods, name)
    for _, method in ipairs(methods) do
        if method.name == name then
            return method
        end
    end
    return nil
end

-- Verify we found all methods
assert(#instance_methods == 5, "Should find 5 instance methods")
assert(#static_methods == 1, "Should find 1 static method")

-- Verify each instance method
assert(find_method(instance_methods, "init"), "Missing init method")
assert(find_method(instance_methods, "process"), "Missing process method")
assert(find_method(instance_methods, "calculate"), "Missing calculate method")
assert(find_method(instance_methods, "append"), "Missing append method")
assert(find_method(instance_methods, "configure"), "Missing configure method")

-- Verify static method
assert(find_method(static_methods, "create"), "Missing create static method")

return {
    instance_methods = instance_methods,
    static_methods = static_methods
}
