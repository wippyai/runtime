local openai = require("openai_client")
local output = require("output")

-- OpenAI Embeddings Handler
local function handler(args)
    -- Validate required arguments
    if not args.input then
        return nil, "Input text is required"
    end

    local model = args.model or "text-embedding-ada-002"

    -- Configure parameters
    local params = {
        encoding_format = args.encoding_format or "float",
        user = args.user,
        dimensions = args.dimensions,
        api_key = args.api_key,
        organization = args.organization,
        timeout = args.timeout or 60,
        base_url = args.endpoint
    }

    -- Make the request using the client's embedding function
    local response, err = openai.create_embeddings(args.input, model, params)

    if err then
        return nil, err.message, { error = err }
    end

    -- Format the response
    if response.data and #response.data > 0 then
        local result = {
            provider = "openai",
            model = model,
            metadata = response.metadata,
            usage = response.usage,
            embeddings = {}
        }

        -- Extract embeddings
        for _, item in ipairs(response.data) do
            table.insert(result.embeddings, {
                embedding = item.embedding,
                index = item.index,
                object = item.object
            })
        end

        return result
    else
        return nil, "No embeddings were returned in the response"
    end
end

-- Return the handler function
return { handler = handler }