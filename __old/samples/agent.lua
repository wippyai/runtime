local time = require("time")
local json = require("json")

-- Agent patterns and helpers library
local Agent = {}

-- Tool name aliasing to enforce API compatibility
Agent.toolNameToFunction = {} -- Tool name (API) to actual function name mapping
Agent.functionToToolName = {} -- Function name to tool name (API) mapping

-- Helper function to create API-safe tool names
function Agent.safeToolName(funcName)
    -- Replace characters not allowed in API tool names with underscores
    local toolName = funcName:gsub("[^a-zA-Z0-9_%-]", "_")

    -- Store in the mapping tables
    Agent.toolNameToFunction[toolName] = funcName
    Agent.functionToToolName[funcName] = toolName

    return toolName
end

-- Common tool schemas
Agent.tools = {
    -- File analyzer tool schema
    file_analyzer = function()
        return {
            name = "analyze_file",
            description = "Analyze a file's content to extract information or insights",
            input_schema = {
                type = "object",
                properties = {
                    filepath = {
                        type = "string",
                        description = "Path to the file to analyze"
                    },
                    analyzer = {
                        type = "string",
                        description = "Type of analyzer to use (markdown, lua, go, html)",
                        enum = { "markdown", "lua", "go", "html" }
                    }
                },
                required = { "filepath" }
            }
        }
    end,

    -- Code execution tool schema
    code_execution = function()
        return {
            name = "execute_code",
            description = "Execute code and return the result",
            input_schema = {
                type = "object",
                properties = {
                    language = {
                        type = "string",
                        description = "Programming language",
                        enum = { "lua", "javascript", "python" }
                    },
                    code = {
                        type = "string",
                        description = "Code to execute"
                    }
                },
                required = { "language", "code" }
            }
        }
    end,

    -- Simple greeting tool schema (for testing)
    greeting = function()
        return {
            name = "greet",
            description = "Generate a greeting for the user",
            input_schema = {
                type = "object",
                properties = {
                    name = {
                        type = "string",
                        description = "Name of the person to greet"
                    },
                    language = {
                        type = "string",
                        description = "Language for the greeting",
                        enum = { "english", "spanish", "french", "german" }
                    }
                },
                required = { "name" }
            }
        }
    end
}

Agent.tools.file_system = function()
    -- Define actual function names
    local funcNames = {
        "tools:tree",
        "tools:read",
        "tools:list",
        "tools:search",
        "tools:analyze"
    }

    return {
        -- Tree tool
        {
            name = Agent.safeToolName(funcNames[1]), -- "tools:tree" -> "tools_tree"
            description = "Generate a tree view of a file system directory structure",
            input_schema = {
                type = "object",
                properties = {
                    path = {
                        type = "string",
                        description = "Directory path to display tree for"
                    },
                    max_depth = {
                        type = "number",
                        description = "Maximum depth to traverse"
                    },
                    extensions = {
                        type = "string",
                        description = "Comma-separated list of file extensions to include"
                    },
                    exclude_dirs = {
                        type = "string",
                        description = "Comma-separated list of directories to exclude"
                    }
                },
                required = { "path" }
            }
        },

        -- Read file tool
        {
            name = Agent.safeToolName(funcNames[2]), -- "tools:read" -> "tools_read"
            description = "Read a file's contents",
            input_schema = {
                type = "object",
                properties = {
                    path = {
                        type = "string",
                        description = "Path to the file to read"
                    },
                    binary_ok = {
                        type = "boolean",
                        description = "Whether to allow reading binary files"
                    }
                },
                required = { "path" }
            }
        },

        -- List directory tool
        {
            name = Agent.safeToolName(funcNames[3]), -- "tools:list" -> "tools_list"
            description = "List contents of a directory",
            input_schema = {
                type = "object",
                properties = {
                    path = {
                        type = "string",
                        description = "Directory path to list"
                    },
                    format = {
                        type = "string",
                        description = "Output format (basic, detailed, json)",
                        enum = { "basic", "detailed", "json" }
                    },
                    show_hidden = {
                        type = "boolean",
                        description = "Whether to show hidden files"
                    }
                },
                required = { "path" }
            }
        },

        -- Search tool
        {
            name = Agent.safeToolName(funcNames[4]), -- "tools:search" -> "tools_search"
            description = "Search for text in files",
            input_schema = {
                type = "object",
                properties = {
                    path = {
                        type = "string",
                        description = "Directory or file path to search in"
                    },
                    pattern = {
                        type = "string",
                        description = "Text pattern to search for"
                    },
                    case_sensitive = {
                        type = "boolean",
                        description = "Whether search should be case-sensitive"
                    },
                    extensions = {
                        type = "string",
                        description = "Comma-separated list of file extensions to search"
                    }
                },
                required = { "path", "pattern" }
            }
        },

        -- Analyze tool
        {
            name = Agent.safeToolName(funcNames[5]), -- "tools:analyze" -> "tools_analyze"
            description = "Analyze a file or directory to get detailed metadata",
            input_schema = {
                type = "object",
                properties = {
                    path = {
                        type = "string",
                        description = "Path to the file or directory to analyze"
                    },
                    format = {
                        type = "string",
                        description = "Output format (text or json)",
                        enum = { "text", "json" }
                    }
                },
                required = { "path" }
            }
        }
    }
end

-- Tool execution functions - these map directly to functions in the system
Agent.tools_executors = {
    -- File analyzer execution function
    analyze_file = function(args)
        local funcs = require("funcs")
        local executor = funcs.new()

        -- Determine which analyzer to use
        local analyzer = args.analyzer or "markdown"
        local function_name = "analyze:" .. analyzer

        -- Call the appropriate analyzer function
        local result, err = executor:call(function_name, args.filepath)
        if err then
            return nil, "Failed to analyze file: " .. err
        end

        -- Extract text content from analyzer result
        if type(result) == "table" and result.text then
            return result.text
        end

        return json.encode(result)
    end,

    -- Greet execution function (for testing)
    greet = function(args)
        local funcs = require("funcs")
        local executor = funcs.new()

        local result, err = executor:call("functions:hello.greet", args.name)
        if err then
            return nil, "Greeting failed: " .. err
        end

        return result
    end
}

-- Tool dispatcher that safely handles tool execution
function Agent.dispatch_tool(tool_name, args)
    -- First check if this tool name is in our mapping
    local function_name = Agent.toolNameToFunction[tool_name] or tool_name

    -- Check if we have an executor for this tool
    local executor = Agent.tools_executors[tool_name]
    if not executor then
        -- Try to directly call a function with this name
        local funcs = require("funcs")
        local executor = funcs.new()

        local result, err = executor:call(function_name, args)
        if err then
            return nil, "Tool execution failed: " .. err
        end

        -- Format result based on type
        if type(result) == "table" then
            if result.text then
                return result.text
            else
                return json.encode(result)
            end
        else
            return tostring(result)
        end
    else
        -- Use the defined executor
        return executor(args)
    end
end

-- System prompts
Agent.system_prompts = {
    -- Code assistant system prompt
    code_assistant = [[
You are Claude, an AI coding assistant that helps users with programming tasks.
When examining code, first understand the overall structure before diving into details.
When writing code, add descriptive comments explaining your approach.
If you need to analyze a file, use the analyze_file tool to examine its contents.
]],

    -- Tool use examples prompt
    tool_use = [[
You can use tools to help accomplish the user's requests.
For files, use the analyze_file tool to examine the content.
For simple greetings, use the greet tool.
Always respond to the results of tool calls with useful insights.
]],

    -- Documentation assistant prompt
    documentation = [[
You are Claude, an AI documentation assistant.
You help users understand system architecture, components, and interactions.
When examining documentation, focus on explaining concepts clearly.
Use the analyze_file tool to read markdown documentation files.
]]
}

-- Create a new agent with tool capabilities
function Agent.new(system_prompt, tools)
    return {
        system_prompt = system_prompt,
        tools = tools or {},
        add_tool = function(self, tool)
            table.insert(self.tools, tool)
            return self
        end,
        get_system_prompt = function(self)
            return self.system_prompt
        end,
        get_tools = function(self)
            return self.tools
        end
    }
end

-- Create an interactive agent for the TUI
function Agent.create_interactive(system_prompt_key)
    local system_prompt = Agent.system_prompts[system_prompt_key] or Agent.system_prompts.tool_use

    -- Define default tools for interactive agent
    local default_tools = {
        Agent.tools.file_analyzer(),
        Agent.tools.greeting()
    }

    -- Add file system tools
    local file_tools = Agent.tools.file_system()
    for _, tool in ipairs(file_tools) do
        table.insert(default_tools, tool)
    end

    return Agent.new(system_prompt, default_tools)
end

-- Extract key information from a Claude response
function Agent.extract_info(response)
    local info = {
        text = "",
        tool_use = nil
    }

    if not response or not response.content then
        return info
    end

    for _, block in ipairs(response.content) do
        if block.type == "text" then
            info.text = info.text .. block.text
        elseif block.type == "tool_use" then
            info.tool_use = {
                id = block.id,
                name = block.name,
                input = block.input
            }
        end
    end

    return info
end

-- Helper to format messages for debugging
function Agent.format_message(message)
    if not message then return "nil" end

    local formatted = {
        role = message.role,
        content = {}
    }

    for _, block in ipairs(message.content or {}) do
        if block.type == "text" then
            table.insert(formatted.content, {
                type = "text",
                text = block.text:sub(1, 50) .. (block.text:len() > 50 and "..." or "")
            })
        elseif block.type == "tool_use" then
            table.insert(formatted.content, {
                type = "tool_use",
                name = block.name,
                input = block.input
            })
        elseif block.type == "tool_result" then
            table.insert(formatted.content, {
                type = "tool_result",
                tool_use_id = block.tool_use_id,
                content = block.content:sub(1, 50) .. (block.content:len() > 50 and "..." or "")
            })
        end
    end

    return json.encode(formatted)
end

return Agent
