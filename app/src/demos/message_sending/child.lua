local http_client = require("http_client")

local function get_local_time()

    return "hello world"

    --local response, err = http_client.get("http://localhost:8082/api/v1/time/local")
    --
    --if err then
    --    return nil, "HTTP request failed: " .. tostring(err)
    --end
    --
    --if response.status_code ~= 200 then
    --    return nil, "HTTP request returned non-200 status: " .. tostring(response.status_code)
    --end
    --
    --return response.body, nil
end

return {
    get_local_time = get_local_time
}
