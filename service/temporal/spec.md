```lua
-- Example of how it would work in Lua
function process_data()
    -- Create a command channel
    local cmd = command.new("http_request", {
        method = "GET",
        url = "https://api.example.com/data"
    })

    -- Commands automatically create a response channel
    local response = cmd:response()
    
    -- We can use normal channel operations to handle responses
    local result = channel.select{
        time.after("5s"):case_receive(),
        timeout_ch:case_receive()  -- Could add timeout handling
    }
    
    if result.channel == response then
        -- Handle API response
        process_result(result.value)
    end
    
    -- We could also subscribe to system events
    local events = command.subscribe("system.disk_usage")
    while true do
        local event = events:receive()
        if event.usage > 90 then
            alert("Disk space critical!")
        end
    end
end

-- Multiple commands can be handled concurrently
coroutine.spawn(function()
local cmd1 = command.new("db_query", {query = "SELECT * FROM users"})
local cmd2 = command.new("cache_get", {key = "config"})

    -- Wait for either response
    local result = channel.select{
        cmd1:response():case_receive(),
        cmd2:response():case_receive()
    }
    
    -- Process whichever came first
    handle_result(result)
end)
```