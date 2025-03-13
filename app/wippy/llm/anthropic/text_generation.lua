local claude = require("claude_client")
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

    -- Process messages - separating system, developer, and cache marker messages from regular messages
    local processed_messages = {}
    local system_content = {}

    -- Track cache markers and their positions
    local cache_marker_positions = {}
    local current_marker_idx = 1
    local has_cache_markers = false

    -- Debug token count issue detection
    local has_system_content = false

    -- First pass: Process system messages and add regular messages
    for i, msg in ipairs(messages) do
        if msg.role == "system" then
            has_system_content = true
            -- Handle system messages - add to system content
            if type(msg.content) == "string" then
                table.insert(system_content, {
                    type = "text",
                    text = msg.content
                })
            else
                -- If content is an array, add each part
                for _, part in ipairs(msg.content) do
                    table.insert(system_content, part)
                end
            end

            -- Track this position for potential system cache markers
            current_marker_idx = #system_content
        elseif msg.role == "cache_marker" then
            -- Found a cache marker - record its position
            table.insert(cache_marker_positions, current_marker_idx)
            has_cache_markers = true
            -- Don't add this to processed messages
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

            -- Track position for potential message cache markers
            -- (This will be used for future implementation of message-level cache markers)
            current_marker_idx = #processed_messages
        end
    end

    -- Second pass: Process developer instructions by attaching them to the previous message
    for i = #messages, 1, -1 do -- Process in reverse to avoid index issues
        local msg = messages[i]

        if msg.role == "developer" then
            -- Get developer instruction content
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

            -- Find the previous non-developer, non-cache_marker message index
            local prev_msg_idx = i - 1
            while prev_msg_idx >= 1 do
                local prev_role = messages[prev_msg_idx].role
                if prev_role ~= "developer" and prev_role ~= "cache_marker" then
                    break
                end
                prev_msg_idx = prev_msg_idx - 1
            end

            -- If we found a valid previous message, append the developer instruction to it
            if prev_msg_idx >= 1 and prev_msg_idx <= #processed_messages then
                -- Find the corresponding processed message
                local processed_idx = 0
                local cur_reg_msg = 0

                -- Find the matching processed message index
                for j = 1, #messages do
                    if j == prev_msg_idx then
                        processed_idx = cur_reg_msg
                        break
                    end

                    if messages[j].role ~= "developer" and messages[j].role ~= "cache_marker" and messages[j].role ~= "system" then
                        cur_reg_msg = cur_reg_msg + 1
                    end
                end

                if processed_idx > 0 and processed_idx <= #processed_messages then
                    -- Get the last content block
                    local last_content_idx = #processed_messages[processed_idx].content
                    if last_content_idx > 0 then
                        local last_content = processed_messages[processed_idx].content[last_content_idx]

                        -- If it's a text block, append the developer instruction
                        if last_content.type == "text" then
                            last_content.text = last_content.text ..
                                "\n<developer-instruction>" .. dev_content .. "</developer-instruction>"
                        end
                    end
                end
            end
        end
    end

    -- Note: Claude prompt caching doesn't require a beta feature flag
    -- It works automatically when cache_control parameters are included in the request

    -- Apply cache markers to system blocks at the recorded positions
    if has_cache_markers and #system_content > 0 then
        -- If we have specific positions, use them
        if #cache_marker_positions > 0 then
            -- We can have up to 4 cache markers, according to Claude documentation
            for i = 1, math.min(#cache_marker_positions, 4) do
                local pos = cache_marker_positions[i]
                -- Only apply if the position is valid for system content
                if pos > 0 and pos <= #system_content then
                    system_content[pos].cache_control = {
                        type = "ephemeral"
                    }
                    print("Applied cache_control to system block at position " .. pos)
                end
            end
        end

        -- If no valid positions were applied (or no positions were specified),
        -- apply to the last system block as fallback
        local applied = false
        for _, block in ipairs(system_content) do
            if block.cache_control then
                applied = true
                break
            end
        end

        if not applied then
            system_content[#system_content].cache_control = {
                type = "ephemeral"
            }
            print("Applied fallback cache_control to last system block")
        end
    end

    -- Configure request payload
    local payload = {
        model = args.model,
        messages = processed_messages,
        max_tokens = options.max_tokens,
        temperature = options.temperature,
        stop_sequences = options.stop_sequences
    }

    -- Only add system content if we have any
    if #system_content > 0 then
        payload.system = system_content
        -- Debug system message token count issue
        print("Adding system content to payload, count:", #system_content)
    elseif has_system_content then
        print("WARNING: System content was detected but not added to payload")
    end

    -- Add thinking if enabled and model supports it
    if options.thinking_effort and options.thinking_effort > 0 then
        if model_supports_thinking(args.model) then
            -- Calculate thinking budget based on thinking effort
            local thinking_budget = claude.calculate_thinking_budget(options.thinking_effort)

            if thinking_budget > 0 then
                -- Ensure max_tokens is greater than thinking budget
                if not payload.max_tokens or payload.max_tokens <= thinking_budget then
                    -- Set max_tokens to thinking budget + 1000 tokens as a reasonable buffer
                    payload.max_tokens = thinking_budget + 1024
                end

                -- Add thinking configuration
                payload.thinking = {
                    type = "enabled",
                    budget_tokens = thinking_budget
                }
            end
        end

        payload.temperature = 1
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
                timeout = args.timeout or 120,
                beta_features = options.beta_features
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
            -- Create token usage object
            tokens = output.usage(
                stream_result.usage.input_tokens or 0,
                stream_result.usage.output_tokens or 0,
                0, -- Claude doesn't return thinking tokens separately
                stream_result.usage.cache_creation_input_tokens or 0,
                stream_result.usage.cache_read_input_tokens or 0
            )

            -- Ensure the cache tokens are directly accessible in the result
            if stream_result.usage.cache_creation_input_tokens and stream_result.usage.cache_creation_input_tokens > 0 then
                tokens.cache_creation_input_tokens = stream_result.usage.cache_creation_input_tokens
            end

            if stream_result.usage.cache_read_input_tokens and stream_result.usage.cache_read_input_tokens > 0 then
                tokens.cache_read_input_tokens = stream_result.usage.cache_read_input_tokens
            end
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
                timeout = args.timeout or 120,
                beta_features = options.beta_features
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
            -- Use output.usage to create a properly formatted token usage object
            tokens = output.usage(
                response.usage.input_tokens or 0,
                response.usage.output_tokens or 0,
                0, -- Claude doesn't return thinking tokens separately
                response.usage.cache_creation_input_tokens or 0,
                response.usage.cache_read_input_tokens or 0
            )

            -- Ensure the cache tokens are directly accessible in the result
            if response.usage.cache_creation_input_tokens and response.usage.cache_creation_input_tokens > 0 then
                tokens.cache_creation_input_tokens = response.usage.cache_creation_input_tokens
            end

            if response.usage.cache_read_input_tokens and response.usage.cache_read_input_tokens > 0 then
                tokens.cache_read_input_tokens = response.usage.cache_read_input_tokens
            end
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
