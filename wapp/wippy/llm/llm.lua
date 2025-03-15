local llm_registry = require("llm_registry")
local stream_helper = require("stream_helper")
local funcs = require("funcs")

-- LLM Library - High-level interface for LLM functionalities
local llm = {}

---------------------------
-- Core LLM Functions
---------------------------

-- Generate text using an LLM model
function llm.generate(system_prompts, user_prompts, options)
    if not options or not options.model then
        return nil, "Model is required in options"
    end

    -- Find implementation
    local implementation, err = llm_registry.find_implementation(
        llm_registry.CAPABILITY.GENERATE,
        options.model
    )

    if not implementation then
        return nil, "No suitable LLM implementation found: " .. (err or "")
    end

    -- Format messages in the expected format
    local messages = {}

    -- Add system prompts
    if system_prompts and #system_prompts > 0 then
        for _, prompt in ipairs(system_prompts) do
            table.insert(messages, {
                role = stream_helper.ROLE.SYSTEM,
                content = prompt
            })
        end
    end

    -- Add user prompts
    if user_prompts and #user_prompts > 0 then
        for _, prompt in ipairs(user_prompts) do
            table.insert(messages, {
                role = stream_helper.ROLE.USER,
                content = prompt
            })
        end
    end

    -- Prepare arguments for the implementation
    local args = {
        messages = messages,
        model = options.model,
        max_tokens = options.max_tokens,
        temperature = options.temperature or 0.5,
        stream_to = options.stream_to
    }

    -- Add any thinking parameters if specified
    if options.thinking then
        args.thinking = options.thinking
    end

    -- Create a funcs executor
    local executor = funcs.new()

    -- Call the implementation
    return executor:call(implementation.id, args)
end

-- Generate text with a schema using an LLM model
function llm.generate_with_schema(schema, system_prompts, user_prompts, options)
    if not options or not options.model then
        return nil, "Model is required in options"
    end

    -- Find implementation
    local implementation, err = llm_registry.find_implementation(
        llm_registry.CAPABILITY.GENERATE_WITH_SCHEMA,
        options.model
    )

    if not implementation then
        return nil, "No suitable LLM implementation found: " .. (err or "")
    end

    -- Format messages in the expected format
    local messages = {}

    -- Add system prompts
    if system_prompts and #system_prompts > 0 then
        for _, prompt in ipairs(system_prompts) do
            table.insert(messages, {
                role = stream_helper.ROLE.SYSTEM,
                content = prompt
            })
        end
    end

    -- Add user prompts
    if user_prompts and #user_prompts > 0 then
        for _, prompt in ipairs(user_prompts) do
            table.insert(messages, {
                role = stream_helper.ROLE.USER,
                content = prompt
            })
        end
    end

    -- Prepare arguments for the implementation
    local args = {
        messages = messages,
        schema = schema,
        model = options.model,
        max_tokens = options.max_tokens,
        temperature = options.temperature or 0.5,
        stream_to = options.stream_to
    }

    -- Add any thinking parameters if specified
    if options.thinking then
        args.thinking = options.thinking
    end

    -- Create a funcs executor
    local executor = funcs.new()

    -- Call the implementation
    return executor:call(implementation.id, args)
end

-- Generate text with tools using an LLM model
function llm.generate_with_tools(system_prompts, user_prompts, tools, options)
    if not options or not options.model then
        return nil, "Model is required in options"
    end

    -- Find implementation
    local implementation, err = llm_registry.find_implementation(
        llm_registry.CAPABILITY.GENERATE_WITH_TOOLS,
        options.model
    )

    if not implementation then
        return nil, "No suitable LLM implementation found: " .. (err or "")
    end

    -- Format messages in the expected format
    local messages = {}

    -- Add system prompts
    if system_prompts and #system_prompts > 0 then
        for _, prompt in ipairs(system_prompts) do
            table.insert(messages, {
                role = stream_helper.ROLE.SYSTEM,
                content = prompt
            })
        end
    end

    -- Add user prompts
    if user_prompts and #user_prompts > 0 then
        for _, prompt in ipairs(user_prompts) do
            table.insert(messages, {
                role = stream_helper.ROLE.USER,
                content = prompt
            })
        end
    end

    -- Prepare arguments for the implementation
    local args = {
        messages = messages,
        tools = tools,
        model = options.model,
        max_tokens = options.max_tokens,
        temperature = options.temperature or 0.5,
        stream_to = options.stream_to
    }

    -- Add any thinking parameters if specified
    if options.thinking then
        args.thinking = options.thinking
    end

    -- Create a funcs executor
    local executor = funcs.new()

    -- Call the implementation
    return executor:call(implementation.id, args)
end

return llm