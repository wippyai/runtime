local openai = require("openai_client")
local output = require("wippy.llm:output")
local json = require("json")

-- OpenAI Schema-Based Generation Handler
-- Forces the model to return responses that match a given JSON schema
local function handler(args)
    -- Validate required arguments
    if not args.model then
        return nil, "Model is required"
    end

    if not args.schema then
        return nil, "Schema is required for schema-based generation"
    end

    -- Format messages from various input formats
    local messages = {}

    -- If messages array provided directly, use it
    if args.messages and #args.messages > 0 then
        messages = args.messages
    else
        -- Otherwise build from separate fields
        -- Add system prompt if provided
        if args.system_prompt then
            table.insert(messages, {
                role = "system",
                content = args.system_prompt
            })
        end

        -- Add user message
        if args.message then
            table.insert(messages, {
                role = "user",
                content = args.message
            })
        end
    end

    if #messages == 0 then
        return nil, "No messages provided"
    end

    -- Create schema explanation for forcing structured output
    local schema_json = type(args.schema) == "string"
                       and args.schema
                       or json.encode(args.schema)

    -- Add schema enforcement through system message
    local had_system = false
    for i, msg in ipairs(messages) do
        if msg.role == "system" then
            had_system = true
            messages[i].content = messages[i].content ..
                "\n\nYou MUST respond with a JSON object that conforms to the following schema:\n" ..
                schema_json
            break
        end
    end

    -- If no system message was found, add one
    if not had_system then
        table.insert(messages, 1, {
            role = "system",
            content = "You MUST respond with a JSON object that conforms to the following schema:\n" ..
                      schema_json
        })
    end

    -- Configure parameters for OpenAI request
    local params = {
        model = args.model,
        temperature = args.temperature,
        top_p = args.top_p,
        frequency_penalty = args.frequency_penalty,
        presence_penalty = args.presence_penalty,
        max_tokens = args.max_tokens,
        timeout = args.timeout,
        api_key = args.api_key,
        organization = args.organization,
        base_url = args.endpoint and args.endpoint:match("(.-)/*$"),
        reasoning_effort = args.reasoning_effort,
        max_completion_tokens = args.max_completion_tokens,
        response_format = { type = "json_object" }
    }

    -- Make non-streaming request - schema generation doesn't need streaming
    local response, err = openai.chat_completion(messages, args.model, params)

    if err then
        return nil, err.message, { error = err }
    end

    -- Format the response
    local formatted_response = openai.format_completion_response(response, {
        model = args.model
    })

    if not formatted_response then
        return nil, "Failed to format OpenAI response"
    end

    -- Extract content and try to parse as JSON
    local first_choice = formatted_response.choices[1]
    if first_choice and first_choice.message and first_choice.message.content then
        local content = first_choice.message.content
        local parsed_json = nil

        local success, result = pcall(json.decode, content)
        if success then
            parsed_json = result
        else
            return nil, "Failed to parse schema-conforming JSON from OpenAI response", formatted_response
        end

        -- Return both the raw content and parsed JSON
        return {
            raw_content = content,
            json = parsed_json,
            provider = "openai",
            model = args.model,
            metadata = formatted_response.metadata,
            usage = formatted_response.usage
        }
    else
        return nil, "No content in OpenAI response"
    end
end

-- Return the handler function
return { handler = handler }