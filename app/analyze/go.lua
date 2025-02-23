local M = {}

-- Helper function for safe text extraction
local function safe_text(str)
    return str or "unknown"
end

-- Helper function for safe list formatting
local function format_list(items, formatter)
    local result = {}
    for _, item in ipairs(items or {}) do
        local formatted = formatter(item)
        if formatted then
            table.insert(result, formatted)
        end
    end
    return table.concat(result, "\n")
end

function M.analyze(filepath)
    -- Get required modules
    local fs = require("fs")
    local treesitter = require("treesitter")
    local myfs = fs.get("system:core")

    if not myfs then
        return nil, "Failed to get filesystem"
    end

    local content = myfs:readfile(filepath)
    if not content then
        return nil, "Failed to read file"
    end

    print("Debug: Creating parser...")
    local parser = treesitter.parser()
    if not parser then
        return nil, "Failed to create parser"
    end

    print("Debug: Setting language to Go...")
    local ok, err = pcall(function()
        parser:set_language("go")
    end)
    if not ok then
        print("Error setting language:", err)
        return nil, "Failed to set Go language: " .. tostring(err)
    end

    print("Debug: Parsing Go file...")
    local tree = parser:parse(content)
    if not tree then
        return nil, "Failed to parse file"
    end

    local root = tree:root_node()
    if root:has_error() then
        print("Warning: Syntax errors found in file")
    end

    -- Initialize analysis structure
    local analysis = {
        package = "",
        imports = {},
        functions = {},
        structs = {},
        interfaces = {},
        methods = {},
        constants = {},
        variables = {},
        errors = {},
        doc_comments = {},
        metrics = {
            total_lines = 0,
            total_functions = 0,
            total_structs = 0,
            total_interfaces = 0,
            total_methods = 0
        }
    }

    print("Debug: Creating queries...")

    -- Package query
    local package_query, package_err = treesitter.query("go", [[
        (source_file
            (package_clause
                (package_identifier) @package.name))
    ]])
    if not package_query then
        print("Error creating package query:", package_err)
        return nil, "Failed to create package query: " .. tostring(package_err)
    end

    -- Import query
    local import_query, import_err = treesitter.query("go", [[
        (import_declaration
            (import_spec_list
                (import_spec
                    path: (interpreted_string_literal) @import.path
                    name: (package_identifier)? @import.alias)))
    ]])
    if not import_query then
        print("Error creating import query:", import_err)
        return nil, "Failed to create import query: " .. tostring(import_err)
    end

    -- Function query
    local function_query, function_err = treesitter.query("go", [[
        (function_declaration
            name: (identifier) @function.name
            parameters: (parameter_list) @function.params
            result: [
                (parameter_list) @function.return_params
                (type_identifier)? @function.return_type
            ]?)
    ]])
    if not function_query then
        print("Error creating function query:", function_err)
        return nil, "Failed to create function query: " .. tostring(function_err)
    end

    -- Struct query
    local struct_query, struct_err = treesitter.query("go", [[
        (type_declaration
            (type_spec
                name: (type_identifier) @struct.name
                type: (struct_type
                    (field_declaration_list)? @struct.fields)))
    ]])
    if not struct_query then
        print("Error creating struct query:", struct_err)
        return nil, "Failed to create struct query: " .. tostring(struct_err)
    end

    print("Debug: Processing package information...")
    local package_matches = package_query:matches(root, content)
    for _, match in ipairs(package_matches) do
        for _, capture in ipairs(match.captures) do
            if capture.name == "package.name" then
                analysis.package = capture.node:text(content)
                print("Debug: Found package:", analysis.package)
            end
        end
    end

    print("Debug: Processing imports...")
    local import_matches = import_query:matches(root, content)
    for _, match in ipairs(import_matches) do
        local import_info = {}
        for _, capture in ipairs(match.captures) do
            if capture.name == "import.path" then
                import_info.path = capture.node:text(content):gsub('"', '')
                print("Debug: Found import path:", import_info.path)
            elseif capture.name == "import.alias" and capture.node then
                import_info.alias = capture.node:text(content)
                print("Debug: Found import alias:", import_info.alias)
            end
        end
        if import_info.path then
            table.insert(analysis.imports, import_info)
        end
    end

    print("Debug: Processing functions...")
    local function_matches = function_query:matches(root, content)
    for _, match in ipairs(function_matches) do
        local func_info = {
            name = "",
            params = {},
            returns = {}
        }
        for _, capture in ipairs(match.captures) do
            if capture.name == "function.name" then
                func_info.name = capture.node:text(content)
                print("Debug: Found function:", func_info.name)
            elseif capture.name == "function.params" then
                func_info.params = capture.node:text(content)
                print("Debug: Function params:", func_info.params)
            elseif capture.name == "function.return_type" and capture.node then
                func_info.returns = capture.node:text(content)
                print("Debug: Function return type:", func_info.returns)
            elseif capture.name == "function.return_params" and capture.node then
                func_info.returns = capture.node:text(content)
                print("Debug: Function return params:", func_info.returns)
            end
        end
        if func_info.name ~= "" then
            table.insert(analysis.functions, func_info)
            analysis.metrics.total_functions = analysis.metrics.total_functions + 1
        end
    end

    print("Debug: Processing structs...")
    local struct_matches = struct_query:matches(root, content)
    for _, match in ipairs(struct_matches) do
        local struct_info = {
            name = "",
            fields = {}
        }
        for _, capture in ipairs(match.captures) do
            if capture.name == "struct.name" then
                struct_info.name = capture.node:text(content)
                print("Debug: Found struct:", struct_info.name)
            elseif capture.name == "struct.fields" and capture.node then
                -- Count the fields by walking through field declarations
                local field_count = capture.node:named_child_count()
                struct_info.fields = field_count
                print("Debug: Struct field count:", field_count)
            end
        end
        if struct_info.name ~= "" then
            table.insert(analysis.structs, struct_info)
            analysis.metrics.total_structs = analysis.metrics.total_structs + 1
        end
    end

    -- Calculate total lines from the root node
    analysis.metrics.total_lines = root:end_point().row + 1

    -- Generate report
    local report = string.format([[
Go Analysis Report (Tree-sitter Enhanced)
---------------------------------------
Package: %s

Metrics:
  Total Lines: %d
  Total Functions: %d
  Total Structs: %d
  Total Methods: %d
  Total Interfaces: %d

Imports (%d):
%s

Functions (%d):
%s

Structs (%d):
%s
]],
        safe_text(analysis.package),
        analysis.metrics.total_lines or 0,
        analysis.metrics.total_functions or 0,
        analysis.metrics.total_structs or 0,
        analysis.metrics.total_methods or 0,
        analysis.metrics.total_interfaces or 0,
        #(analysis.imports or {}),
        format_list(analysis.imports, function(imp)
            if imp and imp.path then
                return string.format("  %s%s",
                    imp.path,
                    imp.alias and " as " .. imp.alias or "")
            end
        end),
        #(analysis.functions or {}),
        format_list(analysis.functions, function(func)
            if func and func.name then
                return string.format("  %s%s%s",
                    func.name,
                    func.params and " " .. func.params or "()",
                    (type(func.returns) == "string" and func.returns ~= "") and " " .. func.returns or "")
            end
        end),
        #(analysis.structs or {}),
        format_list(analysis.structs, function(struct)
            if struct and struct.name then
                return string.format("  %s (%d fields)",
                    struct.name,
                    struct.fields or 0)
            end
        end)
    )

    -- Clean up
    if parser then parser:close() end
    if tree then tree:close() end

    return {
        text = report
    }
end

return M