local json = require("json")
local funcs = require("funcs")

-- Tool Handler
-- Manages the execution of tools and functions
local tool_handler = {}

-- Tool registry - maps tool names to handlers
local tool_registry = {}

-- Internal tools - handled directly (not via external calls)
local internal_tools = {
    ["session:change_model"] = true,
    ["session:change_agent"] = true,
    ["session:clear_history"] = true
}

-- Register a tool handler
function tool_handler.register(tool_name, handler)
    if not tool_name or not handler then
        return false, "Tool name and handler are required"
    end

    tool_registry[tool_name] = handler
    return true
end

-- Unregister a tool handler
function tool_handler.unregister(tool_name)
    if not tool_name then
        return false, "Tool name is required"
    end

    if tool_registry[tool_name] then
        tool_registry[tool_name] = nil
        return true
    end

    return false, "Tool not registered"
end

-- Check if a tool is internal
function tool_handler.is_internal_tool(tool_name)
    return internal_tools[tool_name] ~= nil
end

-- Execute a tool
function tool_handler.execute(tool_name, arguments, context)
    if not tool_name then
        return nil, "Tool name is required"
    end

    -- Check if tool is internal
    if internal_tools[tool_name] then
        -- For internal tools, context must include a method to handle it
        if not context or not context.handle_internal_tool then
            return nil, "Cannot handle internal tool: " .. tool_name
        end

        return context.handle_internal_tool(tool_name, arguments)
    end

    -- Check if tool is registered locally
    if tool_registry[tool_name] then
        local success, result = pcall(tool_registry[tool_name], arguments, context)

        if not success then
            return nil, "Tool execution failed: " .. tostring(result)
        end

        return result
    end

    -- If not registered locally, check if it's a system tool
    if tool_name:match("^system:") then
        return execute_system_tool(tool_name, arguments, context)
    end

    -- If not registered and not a system tool, check if it's a registered function
    return execute_function_tool(tool_name, arguments, context)
end

-- Execute a system tool
function execute_system_tool(tool_name, arguments, context)
    -- This would be replaced with actual system tools implementation
    -- For now, we'll simulate a few system tools

    if tool_name == "system:search" then
        return {
            results = {
                { title = "Sample search result 1", snippet = "This is a sample search result." },
                { title = "Sample search result 2", snippet = "This is another sample search result." }
            }
        }
    elseif tool_name == "system:calculator" then
        if not arguments or not arguments.expression then
            return nil, "Calculator requires an expression"
        end

        -- Extremely simple calculator - only for demo
        local expression = arguments.expression

        -- Only handle basic operations for demo
        local result
        if expression:match("^%d+%s*%+%s*%d+$") then
            local a, b = expression:match("(%d+)%s*%+%s*(%d+)")
            result = tonumber(a) + tonumber(b)
        elseif expression:match("^%d+%s*%-%s*%d+$") then
            local a, b = expression:match("(%d+)%s*%-%s*(%d+)")
            result = tonumber(a) - tonumber(b)
        elseif expression:match("^%d+%s*%*%s*%d+$") then
            local a, b = expression:match("(%d+)%s*%*%s*(%d+)")
            result = tonumber(a) * tonumber(b)
        elseif expression:match("^%d+%s*/%s*%d+$") then
            local a, b = expression:match("(%d+)%s*/%s*(%d+)")
            if tonumber(b) == 0 then
                return nil, "Division by zero"
            end
            result = tonumber(a) / tonumber(b)
        else
            return nil, "Unsupported expression: " .. expression
        end

        return { result = result }
    end

    return nil, "Unknown system tool: " .. tool_name
end

-- Execute a function as a tool
function execute_function_tool(tool_name, arguments, context)
    if not tool_name then
        return nil, "Tool name is required"
    end

    local func = funcs.new()

    -- Check if function exists
    local exists, err = func:exists(tool_name)
    if not exists then
        return nil, "Function not found: " .. tool_name
    end

    -- Call the function
    local result, err = func:call(tool_name, arguments)
    if err then
        return nil, "Function call failed: " .. err
    end

    return result
end

-- Handle tool calls from agent
function tool_handler.handle_tool_calls(tool_calls, session_state, context)
    if not tool_calls or #tool_calls == 0 then
        return true
    end

    local results = {}
    local all_successful = true

    for _, tool_call in ipairs(tool_calls) do
        local tool_name = tool_call.name
        local call_id = tool_call.id
        local arguments = tool_call.arguments

        -- Execute the tool
        local result, err = tool_handler.execute(tool_name, arguments, context)

        -- Store result
        results[call_id] = {
            success = not err,
            result = result,
            error = err,
            tool_name = tool_name
        }

        if err then
            all_successful = false
        end
    end

    return all_successful, results
end

-- Register some default tools
tool_handler.register("web_search", function(arguments)
    -- Simulated web search
    return {
        results = {
            { title = "Search result for: " .. (arguments.query or ""), url = "https://example.com/1" },
            { title = "Another result", url = "https://example.com/2" }
        }
    }
end)

tool_handler.register("calculator", function(arguments)
    if not arguments or not arguments.expression then
        return nil, "Calculator requires an expression"
    end

    -- Safety check - only allow simple math expressions
    if not arguments.expression:match("^[%d%s%+%-%*/%.%(%)]+$") then
        return nil, "Invalid expression"
    end

    -- This is a simplified calculator - in production you'd use a safe math library
    local success, result = pcall(function()
        local func = load("return " .. arguments.expression)
        return func()
    end)

    if not success then
        return nil, "Calculation failed: " .. tostring(result)
    end

    return { result = result }
end)

return tool_handler