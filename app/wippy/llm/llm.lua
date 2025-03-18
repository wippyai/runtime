local models = require("models")
local funcs = require("funcs")

-- LLM Library - High-level interface for LLM functionalities
local llm = {}

-- Allow for dependency injection for testing
llm._models = nil
llm._executor = nil

-- Get models - use injected models or require it
local function get_models()
    return llm._models or models
end

-- Get executor - use injected executor or create new one
local function get_executor()
    return llm._executor or funcs.new()
end

-- Constants for model capabilities
llm.CAPABILITY = models.CAPABILITY

-- Error type constants
llm.ERROR_TYPE = {
    INVALID_REQUEST = "invalid_request",
    AUTHENTICATION = "authentication_error",
    RATE_LIMIT = "rate_limit_exceeded",
    SERVER_ERROR = "server_error",
    CONTEXT_LENGTH = "context_length_exceeded",
    CONTENT_FILTER = "content_filter",
    TIMEOUT = "timeout_error",
    MODEL_ERROR = "model_error" -- Added new error type for invalid model
}

-- Finish/stop reason constants
llm.FINISH_REASON = {
    STOP = "stop",               -- Normal completion
    LENGTH = "length",           -- Reached max tokens
    CONTENT_FILTER = "filtered", -- Content filtered
    TOOL_CALL = "tool_call",     -- Tool/function call
    ERROR = "error"              -- Other error
}

-- Set custom function executor for testing
function llm.set_executor(executor)
    llm._executor = executor
    return llm
end

-- Set custom models module for testing
function llm.set_models(models_module)
    llm._models = models_module
    -- Update CAPABILITY reference to match the injected models
    if models_module and models_module.CAPABILITY then
        llm.CAPABILITY = models_module.CAPABILITY
    end
    return llm
end

-- Internal: Filter options based on model capabilities
function llm._filter_options(options, model_card)
    if not options or not model_card then return {} end

    local filtered = {}
    for k, v in pairs(options) do
        filtered[k] = v
    end

    -- Build capability lookup table
    local capabilities = {}
    for _, cap in ipairs(model_card.capabilities or {}) do
        capabilities[cap] = true
    end

    -- Remove thinking options if not supported
    if filtered.thinking_effort and not capabilities[llm.CAPABILITY.THINKING] then
        filtered.thinking_effort = nil
    end

    -- Remove tool options if not supported
    if not capabilities[llm.CAPABILITY.TOOL_USE] then
        filtered.tool_ids = nil
        filtered.tool_schemas = nil
        filtered.tool_call = nil
    end

    return filtered
end

-- Unified generate function
function llm.generate(prompt_input, options)
    if not options or not options.model then
        return nil, "Model is required in options"
    end

    -- Determine the appropriate capability based on options
    local capability = llm.CAPABILITY.GENERATE
    if options.tool_ids or options.tool_schemas or options.tools then
        capability = llm.CAPABILITY.TOOL_USE
    end

    -- Get model card
    local models_module = get_models()
    local model_card, err = models_module.get_by_name(options.model)
    if not model_card then
        return nil, "Model not found: " .. (err or "unknown error")
    end

    -- Get handler directly from the model_card
    local handler_id = nil
    if capability == llm.CAPABILITY.GENERATE then
        handler_id = model_card.handlers.generate
    elseif capability == llm.CAPABILITY.TOOL_USE then
        handler_id = model_card.handlers.call_tools
    end

    if not handler_id then
        return nil, "Model does not support " .. capability
    end

    -- Handle different types of prompt input
    local messages = {}

    -- If the prompt_input is a prompt builder instance
    if type(prompt_input) == "table" and prompt_input.build and type(prompt_input.build) == "function" then
        -- It's a prompt builder - get messages from it
        local prompt_result = prompt_input:build()
        messages = prompt_result.messages
        -- If the prompt_input is already a built prompt result
    elseif type(prompt_input) == "table" and prompt_input.messages then
        messages = prompt_input.messages
        -- If the prompt_input has get_messages method (prompt builder)
    elseif type(prompt_input) == "table" and prompt_input.get_messages and type(prompt_input.get_messages) == "function" then
        messages = prompt_input:get_messages()
        -- If prompt_input is an array of message objects
    elseif type(prompt_input) == "table" and #prompt_input > 0 then
        -- Trust the message format provided by the user
        messages = prompt_input
        -- Single string - treat as user prompt
    elseif type(prompt_input) == "string" then
        table.insert(messages, {
            role = "user",
            content = prompt_input
        })
    else
        return nil, "Invalid prompt input format"
    end

    -- Filter options based on model capabilities
    local filtered_options = llm._filter_options(options, model_card)

    -- Prepare request
    local request = {
        model = model_card.provider_model, -- This is correct - use provider_model for API calls
        messages = messages,
        options = filtered_options
    }

    -- Add streaming support if provided
    if options.stream then
        request.stream = options.stream
    end

    -- Add tool-specific options if relevant
    if capability == llm.CAPABILITY.TOOL_USE then
        if options.tool_ids then
            request.tool_ids = options.tool_ids
        end

        if options.tool_schemas then
            request.tool_schemas = options.tool_schemas
        end

        if options.tools then
            request.tools = options.tools
        end

        if options.tool_call then
            request.tool_call = options.tool_call
        end
    end

    -- Execute handler using the injected or default executor
    local executor = get_executor()
    local result = executor:call(handler_id, request)

    if result.error then
        return nil, result.error_message or result.error
    end

    if type(result.result) == "table" then
        result.tool_calls = result.result.tool_calls or {}
        result.result = result.result.content or ""

        return result.result
    end

    return result
end

-- Generate structured output (with schema)
function llm.structured_output(schema, prompt_input, options)
    if not options or not options.model then
        return nil, "Model is required in options"
    end

    if not schema then
        return nil, "Schema is required"
    end

    -- Get model card
    local models_module = get_models()
    local model_card, err = models_module.get_by_name(options.model)
    if not model_card then
        return nil, "Model not found: " .. (err or "unknown error")
    end

    -- Get handler directly from the model_card
    local handler_id = model_card.handlers.structured_output
    if not handler_id then
        return nil, "Model does not support structured output"
    end

    -- Handle different types of prompt input (same as generate)
    local messages = {}

    -- If the prompt_input is a prompt builder instance
    if type(prompt_input) == "table" and prompt_input.build and type(prompt_input.build) == "function" then
        -- It's a prompt builder - get messages from it
        local prompt_result = prompt_input:build()
        messages = prompt_result.messages
        -- If the prompt_input is already a built prompt result
    elseif type(prompt_input) == "table" and prompt_input.messages then
        messages = prompt_input.messages
        -- If the prompt_input has get_messages method (prompt builder)
    elseif type(prompt_input) == "table" and prompt_input.get_messages and type(prompt_input.get_messages) == "function" then
        messages = prompt_input:get_messages()
        -- If prompt_input is an array of message objects
    elseif type(prompt_input) == "table" and #prompt_input > 0 then
        -- Trust the message format provided by the user
        messages = prompt_input
        -- Single string - treat as user prompt
    elseif type(prompt_input) == "string" then
        table.insert(messages, {
            role = "user",
            content = prompt_input
        })
    else
        return nil, "Invalid prompt input format"
    end

    -- Filter options based on model capabilities
    local filtered_options = llm._filter_options(options, model_card)

    -- Prepare request
    local request = {
        model = model_card.provider_model, -- Changed to provider_model for consistency
        schema = schema,
        messages = messages,
        options = filtered_options
    }

    -- Execute handler using the injected or default executor
    local executor = get_executor()
    return executor:call(handler_id, request)
end

-- Generate embeddings
function llm.embed(text, options)
    if not options or not options.model then
        return nil, "Model is required in options"
    end

    -- Get model card
    local models_module = get_models()
    local model_card, err = models_module.get_by_name(options.model)
    if not model_card then
        return nil, "Model not found: " .. (err or "unknown error")
    end

    -- Get handler directly from the model_card
    local handler_id = model_card.handlers.embeddings
    if not handler_id then
        return nil, "Model does not support embeddings"
    end

    -- Prepare request
    local request = {
        model = model_card.provider_model, -- Changed to provider_model for consistency
        input = text,
        dimensions = options.dimensions or model_card.dimensions,
        options = llm._filter_options(options, model_card)
    }

    -- Execute handler using the injected or default executor
    local executor = get_executor()
    return executor:call(handler_id, request)
end

-- List available models with specified capability
function llm.available_models(capability)
    local models_module = get_models()
    local all_models = models_module.get_all()

    if not capability then
        return all_models
    end

    -- Filter models by capability
    local filtered = {}
    for _, model in ipairs(all_models) do
        -- First, check if the model has the requested capability in its capabilities list
        local has_capability = false
        if model.capabilities then
            for _, cap in ipairs(model.capabilities) do
                if cap == capability then
                    has_capability = true
                    break
                end
            end
        end

        -- Then check if it has the corresponding handler
        local has_handler = false
        if capability == llm.CAPABILITY.GENERATE and model.handlers and model.handlers.generate then
            has_handler = true
        elseif capability == llm.CAPABILITY.TOOL_USE and model.handlers and model.handlers.call_tools then
            has_handler = true
        elseif capability == llm.CAPABILITY.STRUCTURED_OUTPUT and model.handlers and model.handlers.structured_output then
            has_handler = true
        elseif capability == llm.CAPABILITY.EMBED and model.handlers and model.handlers.embeddings then
            has_handler = true
        elseif capability == llm.CAPABILITY.THINKING and model.handlers and model.handlers.generate then
            has_handler = true
        end

        -- Only include models that have both the capability AND the handler
        if has_capability and has_handler then
            table.insert(filtered, model)
        end
    end

    return filtered
end

-- Get models grouped by provider
function llm.models_by_provider(capability)
    -- Use the models module function
    local models_module = get_models()
    local providers = models_module.get_by_provider()

    if not capability then
        return providers
    end

    -- Filter each provider's models by capability
    for provider_name, provider in pairs(providers) do
        local filtered_models = {}

        for _, model in ipairs(provider.models) do
            -- First, check if the model has the capability
            local has_capability = false
            if model.capabilities then
                for _, cap in ipairs(model.capabilities) do
                    if cap == capability then
                        has_capability = true
                        break
                    end
                end
            end

            -- Then check if it has the corresponding handler
            local has_handler = false
            if capability == llm.CAPABILITY.GENERATE and model.handlers and model.handlers.generate then
                has_handler = true
            elseif capability == llm.CAPABILITY.TOOL_USE and model.handlers and model.handlers.call_tools then
                has_handler = true
            elseif capability == llm.CAPABILITY.STRUCTURED_OUTPUT and model.handlers and model.handlers.structured_output then
                has_handler = true
            elseif capability == llm.CAPABILITY.EMBED and model.handlers and model.handlers.embeddings then
                has_handler = true
            end

            -- Only include models that have both the capability AND the handler
            if has_capability and has_handler then
                table.insert(filtered_models, model)
            end
        end

        -- Update provider's models list
        provider.models = filtered_models
    end

    return providers
end

return llm
