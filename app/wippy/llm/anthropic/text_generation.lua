local claude = require("claude")
local output = require("output")
local json = require("json")

-- Helper function to check if a model supports thinking
local function model_supports_thinking(model)
    -- Currently, only Claude 3.7 models support extended thinking
    if not model then
        return false
    end

    return model:match("claude%-3%-7") or model:match("claude%-3%.7")
end

-- Helper function to apply cache control markers
local function apply_cache_marker(content_block)
    if type(content_block) ~= "table" then
        return content_block
    end

    -- Deep copy the block to avoid side effects
    local block_copy = {}
    for k, v in pairs(content_block) do
        block_copy[k] = v
    end

    -- Add cache control if not already present
    if not block_copy.cache_control then
        block_copy.cache_control = { type = "ephemeral" }
    end

    return block_copy
end

-- Claude Text Generation Handler
-- Supports text generation without tool calling functionality
local function handler(args)
    -- Validate required arguments
    if not args.model then
        return {
            error = output.ERROR_TYPE.INVALID_REQUEST,
            error_message = "Model is required"
        }
    end

    -- Format messages
    local messages = args.messages or {}
    if #messages == 0 then
        return {
            error = output.ERROR_TYPE.INVALID_REQUEST,
            error_message = "No messages provided"
        }
    end

    -- Configure options
    local options = args.options or {}

    -- Process system messages (could be string, array of strings, or array of content blocks)
    local system_content = nil
    local has_developer_instructions = false

    if args.system then
        -- Convert to content blocks format that Claude API expects
        if type(args.system) == "string" then
            -- Single string system prompt
            system_content = { {
                type = "text",
                text = args.system
            } }
        elseif type(args.system) == "table" then
            if args.system.type then
                -- Single content block object
                system_content = { args.system }
            else
                -- Array of content blocks or strings
                system_content = {}
                for i, item in ipairs(args.system) do
                    if type(item) == "string" then
                        table.insert(system_content, {
                            type = "text",
                            text = item
                        })
                    else
                        table.insert(system_content, item)
                    end
                end
            end
        end
    end

    -- Apply cache markers if requested
    if options.cache_marker and system_content and #system_content > 0 then
        -- Apply cache control to the last system block
        local last_block = system_content[#system_content]
        if last_block then
            -- Apply cache marker only if not already present
            if not last_block.cache_control then
                last_block.cache_control = { type = "ephemeral" }
            end
        end
    end

    -- Process messages - separating developer messages from regular messages
    local processed_messages = {}
    local prev_user_idx = nil
    local developer_instructions = {}

    for i, msg in ipairs(messages) do
        if msg.role == "developer" then
            has_developer_instructions = true

            -- Collect developer instructions - we'll add them to system content
            local dev_content
            if type(msg.content) == "string" then
                dev_content = msg.content
            else
                -- If content is an array, extract the text
                local text = ""
                for _, part in ipairs(msg.content) do
                    if part.type == "text" then
                        text = text .. part.text
                    end
                end
                dev_content = text
            end

            table.insert(developer_instructions, dev_content)
        else
            -- Regular user or assistant message
            local role = msg.role
            local content

            -- Format content properly
            if type(msg.content) == "string" then
                content = { { type = "text", text = msg.content } }
            else
                content = msg.content
            end

            -- Add to processed messages
            table.insert(processed_messages, {
                role = role,
                content = content
            })

            -- Track the last user message position
            if role == "user" then
                prev_user_idx = #processed_messages
            end
        end
    end

    -- If we have developer instructions, add them to system content
    if has_developer_instructions and #developer_instructions > 0 then
        if not system_content then
            system_content = {}
        end

        -- Combine all developer instructions into a single system block
        local combined_instructions = "Developer instructions:\n" .. table.concat(developer_instructions, "\n\n")
        table.insert(system_content, {
            type = "text",
            text = combined_instructions
        })
    end

    -- Configure request payload
    local payload = {
        model = args.model,
        messages = processed_messages,
        max_tokens = options.max_tokens,
        temperature = options.temperature,
        system = system_content,
        stop_sequences = options.stop_sequences
    }

    -- Add thinking if enabled and model supports it
    if options.thinking_effort and options.thinking_effort > 0 then
        if model_supports_thinking(args.model) then
            -- Calculate thinking budget based on thinking effort
            local thinking_budget = claude.calculate_thinking_budget(options.thinking_effort)

            if thinking_budget > 0 then
                payload.thinking = {
                    type = "enabled",
                    budget_tokens = thinking_budget
                }
            end
        end
    end

    -- Handle streaming if requested
    if args.stream and args.stream.reply_to then
        -- Create a streamer with the provided reply_to process ID
        local streamer = output.streamer(
            args.stream.reply_to,
            args.stream.topic or "llm_response",
            args.stream.buffer_size or 10
        )

        -- Make streaming request
        local response, err = claude.request(
            claude.API_ENDPOINTS.MESSAGES,
            payload,
            {
                api_key = args.api_key,
                api_version = args.api_version,
                stream = true,
                timeout = args.timeout or 120
            }
        )

        -- Handle request errors
        if err then
            local mapped_error = claude.map_error(err)

            streamer:send_error(
                mapped_error.error,
                mapped_error.error_message,
                mapped_error.code
            )

            return mapped_error
        end

        -- Variables to store the full result
        local full_content = ""
        local finish_reason = nil
        local thinking_content = ""
        local has_thinking = false

        -- Process the streaming response
        local stream_content, stream_err, stream_result = claude.process_stream(response, {
            on_content = function(content_chunk)
                full_content = full_content .. content_chunk
                streamer:buffer_content(content_chunk)
            end,
            on_thinking = function(thinking_chunk)
                -- Collect thinking content and stream it
                thinking_content = thinking_content .. thinking_chunk
                has_thinking = true
                streamer:send_thinking(thinking_chunk)
            end,
            on_error = function(error_info)
                -- Convert error to standard format
                local mapped_error = {
                    error = output.ERROR_TYPE.SERVER_ERROR,
                    error_message = error_info.message or "Error processing stream",
                    code = error_info.code
                }

                -- Send error to the streamer
                streamer:send_error(
                    mapped_error.error,
                    mapped_error.error_message,
                    mapped_error.code
                )
            end,
            on_done = function(result)
                -- Flush any remaining content
                streamer:flush()

                -- Save finish reason
                if result.finish_reason then
                    finish_reason = result.finish_reason
                end

                -- Send a simplified done message with minimal info
                local meta = {
                    model = args.model,
                    provider = "anthropic",
                    usage = result.usage
                }

                -- Add thinking info if available
                if has_thinking then
                    meta.has_thinking = true
                end

                streamer:send_done(meta)
            end
        })

        -- Handle streaming errors
        if stream_err then
            return {
                error = output.ERROR_TYPE.SERVER_ERROR,
                error_message = stream_err,
                code = stream_result and stream_result.error and stream_result.error.code,
                streaming = true
            }
        end

        -- Extract tokens from stream_result if available
        local tokens = nil
        if stream_result and stream_result.usage then
            tokens = output.usage(
                stream_result.usage.input_tokens or 0,
                stream_result.usage.output_tokens or 0,
                0, -- Claude doesn't return thinking tokens separately
                stream_result.usage.cache_creation_input_tokens or 0,
                stream_result.usage.cache_read_input_tokens or 0
            )
        end

        -- Map the finish reason to standardized format
        local standardized_finish_reason = claude.FINISH_REASON_MAP[finish_reason] or finish_reason

        -- Return the final content and metadata for the caller
        local result = {
            result = full_content,
            tokens = tokens,
            metadata = response.metadata,
            finish_reason = standardized_finish_reason,
            streaming = true,
            provider = "anthropic",
            model = args.model
        }

        -- Add thinking if it was included
        if has_thinking then
            result.thinking = thinking_content
        end

        return result
    else
        -- Non-streaming request
        local response, err = claude.request(
            claude.API_ENDPOINTS.MESSAGES,
            payload,
            {
                api_key = args.api_key,
                api_version = args.api_version,
                timeout = args.timeout or 120
            }
        )

        -- Handle errors
        if err then
            local mapped_error = claude.map_error(err)
            return mapped_error
        end

        -- Check response validity
        if not response then
            return {
                error = output.ERROR_TYPE.SERVER_ERROR,
                error_message = "Empty response from Claude API"
            }
        end

        if not response.content then
            return {
                error = output.ERROR_TYPE.SERVER_ERROR,
                error_message = "Invalid response structure from Claude API (missing content)"
            }
        end

        -- Extract content from text blocks and keep track of thinking blocks
        local content = ""
        local thinking_content = ""
        local has_thinking = false

        for i, block in ipairs(response.content) do
            if block.type == "text" then
                content = content .. (block.text or "")
            elseif block.type == "thinking" or block.type == "redacted_thinking" then
                -- Store thinking content
                if block.type == "thinking" then
                    thinking_content = thinking_content .. (block.thinking or "")
                end
                has_thinking = true
            end
        end

        -- Extract token usage information with proper output format
        local tokens = nil
        if response.usage then
            tokens = output.usage(
                response.usage.input_tokens or 0,
                response.usage.output_tokens or 0,
                0, -- Claude doesn't return thinking tokens separately
                response.usage.cache_creation_input_tokens or 0,
                response.usage.cache_read_input_tokens or 0
            )
        end

        -- Map finish reason to standardized format
        local raw_finish_reason = response.stop_reason

        local finish_reason = claude.FINISH_REASON_MAP[response.stop_reason] or response.stop_reason

        -- Return successful response with standardized finish reason
        local result = {
            result = content,
            tokens = tokens,
            metadata = response.metadata,
            finish_reason = finish_reason,
            provider = "anthropic",
            model = args.model
        }

        -- Add thinking if it was included
        if has_thinking then
            result.thinking = thinking_content
        end

        return result
    end
end

-- Return the handler function
return { handler = handler }
