local json = require("json")
local funcs = require("funcs")

-- Agent Handler for Claude TUI
local AgentHandler = {}

function AgentHandler.new(app, config)
    local agent = {}

    -- Initialize with default or provided configuration
    agent.config = config or {}
    agent.app = app

    -- Default system prompt
    agent.system_prompt = agent.config.system_prompt or [[
You are Wippy, an AI assistant with access to file system tools.
You can help users interact with their files and directories.
When a user asks about files or directories, use the appropriate tool to help them.
You dont retry on errors but instead give content or error back to user, you are in debug mode,
dont improvise.
]]

    -- Tool name mappings
    agent.tool_name_mapping = {
        ["list_directory"] = "tools:list",
        ["read_file"] = "tools:read",
        ["search_files"] = "tools:search",
        ["tree"] = "tools:tree"
    }

    -- Define tools
    agent.tools = agent.config.tools or {
        -- List directory tool
        {
            name = "list_directory",
            description = "List contents of a directory",
            input_schema = {
                type = "object",
                properties = {
                    path = {
                        type = "string",
                        description = "Directory path to list"
                    },
                    show_hidden = {
                        type = "boolean",
                        description = "Whether to show hidden files"
                    },
                    format = {
                        type = "string",
                        description = "Output format (basic, detailed, json)",
                        enum = { "basic", "detailed", "json" }
                    }
                },
                required = { "path" }
            }
        },
        -- Read file tool
        {
            name = "read_file",
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
        -- Search files tool
        {
            name = "search_files",
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
        -- Tree tool
        {
            name = "tree",
            description = "Generate a tree view of a directory structure",
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
        }
    }

    -- Get tools for API
    agent.get_tools = function(self)
        return self.tools
    end

    -- Get system prompt
    agent.get_system_prompt = function(self)
        return self.system_prompt
    end

    -- Execute a tool using funcs
    agent.execute_tool = function(self, tool_name, tool_input)
        self.app.ui:log_debug(self.app, "Executing tool via funcs: " .. tool_name)

        -- Map the tool name to actual function name if needed
        local function_name = self.tool_name_mapping[tool_name] or tool_name

        -- Check if the function exists
        if not function_name then
            return nil, "Unknown tool: " .. tool_name
        end

        -- Create a funcs executor
        local executor = funcs.new()

        -- Execute the function with tool input
        self.app.ui:log_debug(self.app,
            "Calling function: " .. function_name .. " with input: " .. json.encode(tool_input))

        local result, err = executor:call(function_name, tool_input)
        if err then
            return nil, "Tool execution failed: " .. err
        end

        -- Convert result to string if it's a table
        if type(result) == "table" then
            result = json.encode(result)
        end

        return result
    end

    -- Configure agent
    agent.configure = function(self, options)
        if options.system_prompt then
            self.system_prompt = options.system_prompt
        end

        if options.tools then
            self.tools = options.tools
        end

        if options.tool_mappings then
            for k, v in pairs(options.tool_mappings) do
                self.tool_name_mapping[k] = v
            end
        end

        return self
    end

    return agent
end

return AgentHandler
