function App()
    -- Initialize channels
    local inbox = tasks.channel()
    local done = channel.new()

    -- Configuration
    local window_width = 80
    local window_height = 24
    local response_text = ""
    local is_loading = true
    local cursor = "▋"
    local cursor_visible = true
    local streaming_active = false
    local max_line_width = window_width - 4

    -- Helper functions
    local function center_text(text, width)
        local padding = width - #text
        if padding <= 0 then return text end
        local left_pad = math.floor(padding / 2)
        local right_pad = padding - left_pad
        return string.rep(" ", left_pad) .. text .. string.rep(" ", right_pad)
    end

    local function wrap_text(text, max_width)
        local lines = {}
        local current_line = ""
        local word_buffer = ""

        for i = 1, #text do
            local char = text:sub(i, i)
            if char:match("%s") then
                if #current_line == 0 then
                    current_line = word_buffer
                elseif #current_line + #word_buffer + 1 <= max_width then
                    current_line = current_line .. " " .. word_buffer
                else
                    table.insert(lines, current_line)
                    current_line = word_buffer
                end
                word_buffer = ""
            else
                word_buffer = word_buffer .. char
            end

            if i == #text and #word_buffer > 0 then
                if #current_line == 0 then
                    current_line = word_buffer
                elseif #current_line + #word_buffer + 1 <= max_width then
                    current_line = current_line .. " " .. word_buffer
                else
                    table.insert(lines, current_line)
                    current_line = word_buffer
                end
            end
        end

        if #current_line > 0 then
            table.insert(lines, current_line)
        end

        return lines
    end

    -- Start cursor blink timer
    coroutine.spawn(function()
        local ticker = time.ticker("0.5s")
        while true do
            local result = channel.select{
                ticker:channel():case_receive(),
                done:case_receive()
            }

            if result.channel == done then
                break
            end

            cursor_visible = not cursor_visible
            if streaming_active then
                upstream.send("refresh")
            end
        end
    end)

    -- Query Ollama about French cuisine
    coroutine.spawn(function()
        local ollama_url = "http://100.70.10.9:11434/api/generate"
        local headers = {
            ["Content-Type"] = "application/json"
        }

        local request_body = json.encode({
            model = "mistral:latest",
            prompt = "Tell me about French cuisine and culinary traditions. Include information about: 1) Famous dishes and their origins 2) The importance of French cooking techniques 3) How French cuisine influenced global gastronomy 4) The role of wine in French dining. Please organize the response clearly and keep sentences complete.",
            stream = true
        })

        local response, err = http.post(ollama_url, {
            headers = headers,
            body = request_body,
            stream = {
                buffer_size = 4096
            }
        })

        if err then
            response_text = "Error querying Ollama: " .. err
            is_loading = false
            streaming_active = false
            upstream.send("refresh")
            return
        end

        print("Got response:", response)
        local stream = response and response.stream
        print("Got stream:", stream)

        if stream then
            print("Starting stream read")
            streaming_active = true
            local buffer = ""

            while true do
                print("Reading chunk...")
                local chunk, err = stream:read()
                print("Read result: chunk=", chunk, ", error=", err)
                if err then
                    upstream.log("Stream error: " .. tostring(err))
                    break
                end
                if not chunk then
                    upstream.log("No more chunks")
                    break
                end

                local success, decoded_chunk = pcall(json.decode, chunk)
                if success and decoded_chunk and decoded_chunk.response then
                    buffer = buffer .. decoded_chunk.response
                    if buffer:match("[%.%!%?]%s*$") or #buffer > 50 then
                        response_text = response_text .. buffer
                        buffer = ""
                        upstream.send("refresh")
                        time.sleep("30ms")
                    end
                end
            end

            if #buffer > 0 then
                response_text = response_text .. buffer
                upstream.send("refresh")
            end

            stream:close()
        end

        streaming_active = false
        is_loading = false
        upstream.send("refresh")
    end)

    local function create_box()
        local lines = {
            "┌" .. string.rep("─", window_width-2) .. "┐"
        }

        table.insert(lines, "│" .. center_text("French Cuisine & Culture", window_width-2) .. "│")
        table.insert(lines, "│" .. string.rep("─", window_width-2) .. "│")

        if is_loading then
            if streaming_active then
                local status = "Receiving response" .. string.rep(".", math.floor(time.now():unix() % 4))
                if cursor_visible then
                    status = status .. cursor
                end
                table.insert(lines, "│" .. center_text(status, window_width-2) .. "│")
            else
                table.insert(lines, "│" .. center_text("Connecting to Ollama...", window_width-2) .. "│")
            end
        end

        local wrapped_lines = wrap_text(response_text, max_line_width)
        for _, line in ipairs(wrapped_lines) do
            local display_line = "│ " .. line .. string.rep(" ", max_line_width - #line) .. " │"
            table.insert(lines, display_line)
        end

        if streaming_active and cursor_visible and #wrapped_lines > 0 then
            local last_line = lines[#lines]
            lines[#lines] = last_line:sub(1, -3) .. cursor .. "│"
        end

        while #lines < window_height-1 do
            table.insert(lines, "│" .. string.rep(" ", window_width-2) .. "│")
        end

        table.insert(lines, "└" .. string.rep("─", window_width-2) .. "┘")

        return table.concat(lines, "\n")
    end

    while true do
        local task, ok = inbox:receive()
        if not ok then
            done:send(true)
            break
        end

        local msg = task:input()

        if msg.type == "update" then
            task:complete("ok")
        elseif msg.type == "view" then
            local view = create_box()
            task:complete(view)
        else
            task:complete("ok")
        end
    end

    done:close()
end

return App