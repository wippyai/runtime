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

    print("Debug: Setting language to Lua...")
    local ok, err = pcall(function()
        parser:set_language("lua")
    end)
    if not ok then
        print("Error setting language:", err)
        return nil, "Failed to set Lua language: " .. tostring(err)
    end

    print("Debug: Parsing Lua file...")
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
        functions = {},
        requires = {},
        variables = {},
        comments = {
            doc = {},    -- Documentation comments
            inline = {}  -- Regular inline comments
        },
        metrics = {
            total_lines = 0,
            total_functions = 0,
            total_requires = 0,
            total_variables = 0,
            total_comments = 0
        }
    }

    print("Debug: Creating queries...")

    -- Function query
    local function_query, function_err = treesitter.query("lua", [[
        (function_declaration
            name: (identifier) @func.name
            parameters: (parameters) @func.params) @func.decl

        (variable_declaration
            (assignment_statement
                (variable_list
                    (identifier) @func.name)
                (expression_list
                    (function_definition) @func.def)))
    ]])
    if not function_query then
        print("Error creating function query:", function_err)
        return nil, "Failed to create function query: " .. tostring(function_err)
    end

    -- Require query
    local require_query, require_err = treesitter.query("lua", [[
        (function_call
            (identifier) @req.func
            (arguments
                (string) @req.module)
            (#eq? @req.func "require"))
    ]])
    if not require_query then
        print("Error creating require query:", require_err)
        return nil, "Failed to create require query: " .. tostring(require_err)
    end

    -- Variable query (including local declarations)
    local variable_query, variable_err = treesitter.query("lua", [[
        (variable_declaration
            (assignment_statement
                (variable_list
                    (identifier) @var.name)
                (expression_list
                    (_) @var.value)))
    ]])
    if not variable_query then
        print("Error creating variable query:", variable_err)
        return nil, "Failed to create variable query: " .. tostring(variable_err)
    end

    -- Comment query
    local comment_query, comment_err = treesitter.query("lua", [[
        (comment) @comment
    ]])
    if not comment_query then
        print("Error creating comment query:", comment_err)
        return nil, "Failed to create comment query: " .. tostring(comment_err)
    end

    print("Debug: Processing functions...")
    local function_matches = function_query:matches(root, content)
    for _, match in ipairs(function_matches) do
        local func_info = {
            name = "",
            params = "",
            type = "function"
        }
        for _, capture in ipairs(match.captures) do
            if capture.name == "func.name" then
                func_info.name = capture.node:text(content)
            elseif capture.name == "func.params" then
                func_info.params = capture.node:text(content)
            end
        end
        if func_info.name ~= "" then
            table.insert(analysis.functions, func_info)
            analysis.metrics.total_functions = analysis.metrics.total_functions + 1
        end
    end

    print("Debug: Processing requires...")
    local require_matches = require_query:matches(root, content)
    for _, match in ipairs(require_matches) do
        local req_info = {
            module = ""
        }
        for _, capture in ipairs(match.captures) do
            if capture.name == "req.module" then
                req_info.module = capture.node:text(content):gsub('"', ''):gsub("'", '')
            end
        end
        if req_info.module ~= "" then
            table.insert(analysis.requires, req_info)
            analysis.metrics.total_requires = analysis.metrics.total_requires + 1
        end
    end

    print("Debug: Processing variables...")
    local variable_matches = variable_query:matches(root, content)
    for _, match in ipairs(variable_matches) do
        local var_info = {
            name = "",
            value_type = ""
        }
        for _, capture in ipairs(match.captures) do
            if capture.name == "var.name" then
                var_info.name = capture.node:text(content)
            elseif capture.name == "var.value" then
                var_info.value_type = capture.node:type()
            end
        end
        if var_info.name ~= "" then
            table.insert(analysis.variables, var_info)
            analysis.metrics.total_variables = analysis.metrics.total_variables + 1
        end
    end

    print("Debug: Processing comments...")
    local comment_matches = comment_query:matches(root, content)
    for _, match in ipairs(comment_matches) do
        local comment_text = match.captures[1].node:text(content)
        -- Check if it's a documentation comment (---)
        if comment_text:match("^%s*%-%-%-") then
            table.insert(analysis.comments.doc, comment_text)
        else
            table.insert(analysis.comments.inline, comment_text)
        end
        analysis.metrics.total_comments = analysis.metrics.total_comments + 1
    end

    -- Calculate total lines from the root node
    analysis.metrics.total_lines = root:end_point().row + 1

    -- Generate report
    local report = string.format([[
Lua Analysis Report (Tree-sitter Enhanced)
----------------------------------------

Metrics:
  Total Lines: %d
  Total Functions: %d
  Total Requires: %d
  Total Variables: %d
  Total Comments: %d

Required Modules (%d):
%s

Functions (%d):
%s

Variables (%d):
%s

Comments:
Documentation Comments (%d):
%s

Inline Comments (%d):
%s
]],
        analysis.metrics.total_lines or 0,
        analysis.metrics.total_functions or 0,
        analysis.metrics.total_requires or 0,
        analysis.metrics.total_variables or 0,
        analysis.metrics.total_comments or 0,

        #(analysis.requires or {}),
        format_list(analysis.requires, function(req)
            if req and req.module then
                return string.format("  %s", req.module)
            end
        end),

        #(analysis.functions or {}),
        format_list(analysis.functions, function(func)
            if func and func.name then
                return string.format("  %s%s",
                    func.name,
                    func.params or "()")
            end
        end),

        #(analysis.variables or {}),
        format_list(analysis.variables, function(var)
            if var and var.name then
                return string.format("  %s (%s)",
                    var.name,
                    var.value_type or "unknown")
            end
        end),

        #(analysis.comments.doc or {}),
        format_list(analysis.comments.doc, function(comment)
            return "  " .. comment
        end),

        #(analysis.comments.inline or {}),
        format_list(analysis.comments.inline, function(comment)
            return "  " .. comment
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