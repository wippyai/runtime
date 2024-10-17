-- Validate input
if not args.file_name or args.file_name == "" then
    error("File name cannot be empty")
end

-- Read the file content
local file = io.open(args.file_name, "r")
if not file then
    error("Could not open file: " .. args.file_name)
end
local code = file:read("*a")
file:close()

-- Use treesitter to parse the PHP code and query for method declarations
local treesitter = require('treesitter')
local result, query_error = treesitter.query("php", code, [[
(
  (method_declaration
    (visibility_modifier)?
    name: (name) @method.name
    parameters: (formal_parameters) @method.params
    body: (compound_statement) @method.body
  ) @this_is_my_name
)
]])

if query_error then
    error("Error in TreeSitter query: " .. query_error)
end

-- Directly return the result without iteration
return result
