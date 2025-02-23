local json = require("json")
local http = require("http_client")
local env = require("env")

function handler(args)
    -- Format messages from history
    local messages = {}
    for _, msg in ipairs(args.history or {}) do
        table.insert(messages, {
            role = msg.role,
            content = msg.content
        })
    end
    table.insert(messages, {
        role = "user",
        content = args.message
    })

    local headers = {
        ["Content-Type"] = "application/json",
        ["Authorization"] = "Bearer " .. (env.get("OPENAI_KEY") or "")
    }

    local body = json.encode({
        model = args.model or "gpt-4o",
        messages = messages,
        stream = args.stream == true
    })

    if args.stream and args.reply_to then
        local response = http.post(args.endpoint, {
            headers = headers,
            body = body,
            stream = { buffer_size = 4096 }
        })

        local buffer = ""
        local full_response = ""

        if response.stream then
            while true do
                local chunk = response.stream:read()
                if not chunk then break end

                for line in chunk:gmatch("[^\n]+") do
                    if line:sub(1, 5) == "data:" then
                        local data = line:sub(6):match("^%s*(.-)%s*$")
                        if data == "[DONE]" then break end

                        local success, decoded = pcall(json.decode, data)
                        if success and type(decoded) == "table" and
                           type(decoded.choices) == "table" and
                           #decoded.choices > 0 and
                           decoded.choices[1].delta and
                           decoded.choices[1].delta.content then
                            local content = decoded.choices[1].delta.content
                            buffer = buffer .. content
                            full_response = full_response .. content

                            -- Stream chunks when buffer is large or sentence appears complete
                            if #buffer > 10 or buffer:match("[%.%!%?]%s*$") then
                                func.send(args.reply_to, "response", {
                                    text = buffer,
                                    done = false
                                })
                                buffer = ""
                            end
                        end
                    end
                end
            end

            if #buffer > 0 then
                func.send(args.reply_to, "response", {
                    text = buffer,
                    done = false
                })
            end

            func.send(args.reply_to, "response", { done = true })
            response.stream:close()
            return full_response
        end
    else
        local response = http.post(args.endpoint, {
            headers = headers,
            body = body,
            timeout = 900
        })

        if response.status_code >= 200 and response.status_code < 300 then
            local data = json.decode(response.body)
            if data and data.choices and data.choices[1] and data.choices[1].message then
                return data.choices[1].message.content
            end
        end
    end

    return nil, "Failed to get LLM response"
end

return { handler = handler }
