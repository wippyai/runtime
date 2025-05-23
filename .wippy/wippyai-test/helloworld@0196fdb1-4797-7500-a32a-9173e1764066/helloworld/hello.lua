-- hello_world.lua
-- Simple handler that returns a Hello World message

-- Get the required modules
local http = require("http")

-- Main handler function
local function handler()
  -- Get HTTP request and response objects
  local req, err = http.request()
  local res = http.response()

  if err then
    res:set_status(http.STATUS.INTERNAL_ERROR)
    res:write_json({ error = "Failed to create request context", message = err })
    return
  end

  -- Return a simple Hello World message
  res:set_status(http.STATUS.OK)
  res:write_json({
    message = "Hello World",
    timestamp = os.time(),
    status = "success"
  })
end

return handler
