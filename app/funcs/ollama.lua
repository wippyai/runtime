local http = require("http")
local json = require("json")
local httpctx = require("httpctx")

local default_ollama_url = "http://127.0.0.1:11434/api/generate"

function ollama_handler()
    local res = httpctx.response()
    local req = httpctx.request()

    -- Extract parameters from the incoming request (you can modify how these are obtained)
    local model = req:query("model") or "mistral:latest" -- Get model from query parameter, default to phi3:14b
    local prompt = req:query("prompt")

    if not prompt or prompt == "" then
        res:set_status(400) -- Bad Request
        res:write("Error: Prompt is required in the request body")
        return
    end

    local options = {
        buffer_size = tonumber(req:query("buffer_size")) or 4096,
        max_size = tonumber(req:query("max_size")), -- Optional
        timeout = tonumber(req:query("timeout")) or 5000,
        ollama_url = req:query("ollama_url") or default_ollama_url
    }

    local function query_ollama_and_stream_response(response, model, prompt, options)
        options = options or {}
        local ollama_url = options.ollama_url or default_ollama_url
        local headers = {
            ["Content-Type"] = "application/json"
        }

        local request_body = json.encode({
            model = model,
            prompt = prompt,
            stream = true -- Always stream from Ollama
        })

        local ollama_response, err = http.post(ollama_url, {
            headers = headers,
            body = request_body,
            stream = {
                buffer_size = options.buffer_size,
                max_size = options.max_size,
                timeout = options.timeout
            }
        })

        if err then
            response:set_status(500) -- Internal Server Error
            return "Error querying Ollama: " .. err
        end

        if ollama_response.status_code ~= 200 then
            response:set_status(ollama_response.status_code)
            response:write("Ollama API error: " .. ollama_response.status_code .. " - " .. (ollama_response.body or ""))
            return
        end

        -- Set up chunked transfer encoding for the user's response
        response:set_transfer(httpctx.TRANSFER.CHUNKED)
        response:set_header("Content-Type", "application/json") -- Or text/plain if you want to stream raw text

        -- Stream the response from Ollama back to the user
        local stream = ollama_response.stream
        if not stream then
            response:set_status(500) -- Internal Server Error
            return
        end

        for chunk in stream() do
            if chunk == nil then break end -- End of Ollama stream

            -- Decode the JSON chunk from Ollama (if it's JSON) and extract the "response" field
            local decoded_chunk, decode_err = json.decode(chunk)
            if decode_err then
                break
            end

            if decoded_chunk and decoded_chunk.response then
                -- Write the extracted "response" part to the user
                response:write(decoded_chunk.response)
            else
                -- this is not a response but data about
                response:write(json.encode(decoded_chunk))
            end

            response:flush()
        end

        response:write("")
        response:flush()
    end

    local err = query_ollama_and_stream_response(res, model, prompt, options)
    if err then
        res:set_status(500) -- Internal Server Error
        res:write("Error: " .. err)
    end
end
