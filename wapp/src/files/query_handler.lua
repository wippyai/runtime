local http = require("http")
local security = require("security")
local funcs = require("funcs")
local process = require("process")

-- Query handler function
local function query_handler()
    local req = http.request()
    local res = http.response()

    if not req or not res then
        return nil, "Failed to get HTTP context"
    end

    -- Set JSON content type for response
    res:set_content_type(http.CONTENT.JSON)

    -- Get current user from security context
    local actor = security.actor()
    if not actor then
        res:set_status(http.STATUS.UNAUTHORIZED)
        res:write_json({
            success = false,
            error = "Authentication required"
        })
        return
    end

    -- Parse request body
    local body, err = req:body_json()
    if err then
        res:set_status(http.STATUS.BAD_REQUEST)
        res:write_json({
            success = false,
            error = "Invalid JSON request",
            details = err
        })
        return
    end

    -- Check required parameters
    local file_id = body.file_id
    local query = body.query

    if not file_id or file_id == "" then
        res:set_status(http.STATUS.BAD_REQUEST)
        res:write_json({
            success = false,
            error = "File ID is required"
        })
        return
    end

    if not query or query == "" then
        res:set_status(http.STATUS.BAD_REQUEST)
        res:write_json({
            success = false,
            error = "Query is required"
        })
        return
    end

    -- Prepare arguments for the RAG process
    local args = {
        file_id = file_id,
        query = query,
        user_id = actor:id()
    }

    -- Non-blocking option: Just spawn the process and return a job ID
    if body.async == true then
        -- Spawn a process to handle the query
        local child_pid = process.spawn(
            "app.files:document_rag",  -- Process type
            "app:processes",           -- Host to run on
            args                       -- Arguments
        )

        if not child_pid then
            res:set_status(http.STATUS.INTERNAL_ERROR)
            res:write_json({
                success = false,
                error = "Failed to start document query process"
            })
            return
        end

        -- Return the job ID immediately
        res:set_status(http.STATUS.ACCEPTED)
        res:write_json({
            success = true,
            message = "Query processing started",
            job_id = child_pid,
            status = "processing"
        })
        return
    end

    -- Default: Blocking call that waits for result
    local result, err = funcs.new()
        :with_context({
            actor_id = actor:id(),
            request_id = req:id() or "req-" .. os.time()
        })
        :call("app.files:document_rag", args)

    -- Handle process call failure
    if err then
        res:set_status(http.STATUS.INTERNAL_ERROR)
        res:write_json({
            success = false,
            error = "Failed to process query",
            details = err
        })
        return
    end

    -- Return the result from the process
    if result.success then
        res:set_status(http.STATUS.OK)
    else
        res:set_status(http.STATUS.BAD_REQUEST)
    end

    res:write_json(result)
end

return {
    query_handler = query_handler
}