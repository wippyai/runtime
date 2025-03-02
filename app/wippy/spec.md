# Comprehensive Pony Runtime Knowledge Compression

## 1. Core Architecture

Pony is a distributed process system for resilient AI applications with Erlang-style isolation and supervision:

- **Process Isolation**: PIDs with format `{node@host|namespace:name|procname}`
- **Message Passing**: All communication through messages, never shared memory
- **Supervision**: Hierarchical monitoring with automatic failure recovery
- **Lua Runtime**: Processes run as Lua coroutines with channel-based concurrency

## 2. Process System

### Process Lifecycle

```lua
-- Define a process
local function run(args)
    -- Initialize process
    local pid = process.pid()
    local info = process.info()  -- Get metadata
    
    -- Setup communication channels
    local events = process.events()  -- System events
    local msgs = process.listen("topic")  -- Named topic
    local inbox = process.inbox()  -- FALLBACK for unlistened topics
    
    -- Main event loop
    while true do
        local result = channel.select{
            events:case_receive(),
            msgs:case_receive(),
            inbox:case_receive()  -- Catch-all messages
        }
        
        if result.channel == events then
            local event = result.value
            if event.event.kind == process.EVENT_CANCEL then
                -- Handle cancelation
                break
            elseif event.event.kind == process.EVENT_RESULT then
                -- Handle completion/failure
            end
        elseif result.channel == msgs then
            -- Handle topic-specific message (direct value)
            local value = result.value
            process_message(value)
        elseif result.channel == inbox then
            -- Handle fallback messages (with metadata)
            local msg = result.value
            -- Format: {topic="topic_name", payload={value}}
            process_inbox_message(msg.topic, msg.payload[1])
        end
    end
    
    -- Return result when done
    return {status = "completed", data = result_data}
end
```

### Process Creation and Communication

```lua
-- Spawn processes
local pid = process.spawn("namespace:name", "host", {arg1="value"})
local pid_with_monitor = process.spawn_monitored("namespace:name", "host", {arg1="value"})

-- Send messages (each sent as separate message)
process.send(target_pid, "topic", value1)
process.send(target_pid, "topic", value1, value2)  -- Sends two separate messages

-- Terminate processes
process.terminate(pid)  -- Immediate termination
process.cancel(pid, "30s")  -- Graceful termination with deadline
```

## 3. Channel System

### Channel Types and Operations

```lua
-- Create channels
local unbuffered = channel.new()  -- Blocks until receiver ready
local buffered = channel.new(5)   -- Buffers up to 5 messages

-- Basic operations
buffered:send("message")  -- Blocks if buffer full
local value, ok = buffered:receive()  -- ok=false if channel closed
buffered:close()  -- Close channel, senders get error

-- Select operations (multiplexing)
local result = channel.select{
    ch1:case_receive(),
    ch2:case_send("value"),
    default = true  -- Non-blocking option
}

-- Access result
if result.channel == ch1 then
    -- Handle received value with result.value
elseif result.channel == ch2 then
    -- Send succeeded
elseif result.default then
    -- No operations were ready
end

-- Debug information
local size = ch:_debug_size()  -- Buffer used
local senders = ch:_debug_senders()  -- Blocked senders
local receivers = ch:_debug_receivers()  -- Blocked receivers
```

## 4. Function API

### Function Definition and Communication

```lua
-- Define a function
function handler(args)
    -- Get own PID
    local my_pid = func.pid()
    
    -- Set up inbox for responses
    local inbox = func.inbox()
    
    -- Send messages to processes or other functions
    func.send(target_pid, "topic", payload)
    func.send(target_pid, "topic", value1, value2)  -- Multiple values
    
    -- Receive response
    local msg = inbox:receive()
    -- Format: {topic="response_topic", payload={response_value}}
    
    -- Return result
    return {status = "success", data = msg.payload[1]}
end
```

## 5. HTTP and WebSocket

### HTTP Request Handling

```lua
function handler()
    local req = http.request()
    local res = http.response()
    
    -- Request information
    local method = req:method()  -- GET, POST, etc.
    local path = req:path()
    local query = req:query("param_name")
    local header = req:header("Content-Type")
    local content_type = req:content_type()
    local body = req:body()  -- Raw body
    local json_data = req:body_json()  -- Parsed JSON
    
    -- Response building
    res:set_status(http.STATUS.OK)  -- 200
    res:set_header("X-Custom", "value")
    res:set_content_type(http.CONTENT.JSON)
    res:write_json({result = "success", data = data})
}
```

### HTTP Streaming

```lua
function stream_handler()
    local req = http.request()
    local res = http.response()
    
    -- Set up chunked transfer
    res:set_transfer(http.TRANSFER.CHUNKED)
    res:set_content_type(http.CONTENT.JSON)
    
    -- Stream multiple chunks
    for i = 1, 5 do
        res:write_json({chunk = i, data = generate_data()})
        res:write("\n")  -- Line delimiter
        res:flush()  -- Force send chunk immediately
        time.sleep("1s")
    end
}

-- Stream request body
function handle_large_upload()
    local req = http.request()
    local res = http.response()
    
    -- Stream request body in chunks
    local iterator, err = req:stream_body({buffer_size = 4096})
    if err then
        res:set_status(http.STATUS.BAD_REQUEST)
        return
    end
    
    local total_size = 0
    for chunk in iterator do
        if chunk == nil then break end
        total_size = total_size + #chunk
        -- Process each chunk
    end
    
    res:write_json({result = "processed", size = total_size})
}
```

### Server-Sent Events (SSE)

```lua
function sse_handler()
    local req = http.request()
    local res = http.response()
    
    -- Set up SSE
    res:set_transfer(http.TRANSFER.SSE)
    
    -- Send events
    res:write_event({name = "start", data = {message = "Connected"}})
    
    for i = 1, 10 do
        res:write_event({
            name = "update", 
            data = {progress = i * 10}
        })
        res:flush()
        time.sleep("1s")
    end
    
    res:write_event({name = "complete", data = {message = "Done"}})
}
```

### WebSocket Client

```lua
local client, err = websocket.connect("wss://example.com/socket", {
    headers = {["User-Agent"] = "Pony WebSocket Client"},
    protocols = {"v1", "v2"},
    dial_timeout = "5s"
})

-- Send message
client:send("Hello WebSocket")

-- Receive messages
local ch = client:receive()
coroutine.spawn(function()
    while true do
        local msg, ok = ch:receive()
        if not ok then break end
        
        if msg.type == websocket.TYPE_TEXT then
            -- Handle text message
            process_text(msg.data)
        elseif msg.type == websocket.TYPE_CLOSE then
            -- Handle close
            break
        end
    end
end)

-- Close connection
client:close(websocket.CLOSE_CODES.NORMAL, "Goodbye")
```

## 6. Available Lua Modules

### Time and Date (time)

```lua
-- Current time
local now = time.now()

-- Time formatting
local formatted = now:format(time.RFC3339)
local date = now:format(time.DateOnly)

-- Time parsing
local t, err = time.parse("2006-01-02 15:04:05", "2023-03-15 14:30:00")

-- Duration
local d, err = time.parse_duration("1h30m")
local seconds = d:seconds()  -- 5400
time.sleep("5s")  -- Sleep for 5 seconds

-- Timers
local timer = time.timer("100ms")
local ticker = time.ticker("1s")
local ch = timer:channel()  -- Triggers once
local tick_ch = ticker:channel()  -- Triggers repeatedly
```

### File System (fs)

```lua
local fs = require("fs").default()

-- Directory operations
fs:mkdir("new_dir")
fs:chdir("new_dir")
for entry in fs:readdir(".") do
    -- entry.name, entry.type (fs.type.FILE or fs.type.DIR)
end

-- File operations
local file = fs:open("file.txt", "w")
file:write("Hello world")
file:sync()
file:close()

-- Read entire file
local content = fs:readfile("file.txt")

-- Check operations
local exists = fs:exists("file.txt")
local is_dir = fs:isdir("directory")
local info = fs:stat("file.txt")  -- name, size, mode, modified, is_dir
```

### HTTP Client (http_client)

```lua
-- Simple requests
local response, err = http_client.get("https://api.example.com/data")
local response, err = http_client.post("https://api.example.com/create", {
    body = '{"name":"test"}',
    headers = {["Content-Type"] = "application/json"}
})

-- Request with options
local response, err = http_client.request("PUT", "https://api.example.com/update", {
    headers = {["Authorization"] = "Bearer token"},
    body = '{"status":"active"}',
    timeout = "30s"
})

-- Batch requests
local requests = {
    {"GET", "https://api.example.com/users"},
    {"GET", "https://api.example.com/posts"}
}
local responses, errors = http_client.request_batch(requests)

-- Streaming responses
local response, err = http_client.get("https://example.com/large", {
    stream = {buffer_size = 4096}
})
local stream = response.stream
for chunk in stream do
    -- Process chunk
end
stream:close()
```

### JSON (json)

```lua
-- Encode Lua to JSON
local jsonStr, err = json.encode({name = "test", values = {1, 2, 3}})

-- Decode JSON to Lua
local data, err = json.decode('{"name":"test","values":[1,2,3]}')
```

### Base64 (base64)

```lua
local encoded, err = base64.encode("Hello World")
local decoded, err = base64.decode("SGVsbG8gV29ybGQ=")
```

### Hash (hash)

```lua
local md5_hash, err = hash.md5("data")
local sha256_hash, err = hash.sha256("data")
local fnv32_value, err = hash.fnv32("data")
```

### Crypto (crypto)

```lua
-- Random data
local bytes, err = crypto.random.bytes(32)
local str, err = crypto.random.string(16)
local uuid, err = crypto.random.uuid()

-- HMAC
local hmac, err = crypto.hmac.sha256("key", "data")

-- Encryption
local encrypted, err = crypto.encrypt.aes("data", "key", "aad")
local decrypted, err = crypto.decrypt.aes(encrypted, "key", "aad")

-- JWT
local token, err = crypto.jwt.encode({sub = "user", exp = os.time() + 3600}, "secret")
local payload, err = crypto.jwt.verify(token, "secret")
```

### UUID (uuid)

```lua
-- Generate UUIDs
local id, err = uuid.v4()  -- Random
local id, err = uuid.v7()  -- Time-ordered
local id, err = uuid.v5("namespace", "name")  -- Deterministic

-- UUID operations
local valid, err = uuid.validate(id)
local version, err = uuid.version(id)
local info, err = uuid.parse(id)  -- Components
local formatted, err = uuid.format(id, "simple")  -- Format options
```

### Logger (logger)

```lua
-- Log levels
logger:debug("Debug message", {context = "value"})
logger:info("Info message")
logger:warn("Warning message", {details = "important"})
logger:error("Error message", {error = err})

-- Creating loggers with context
local req_logger = logger:with({request_id = "123"})
req_logger:info("Processing request")

-- Named loggers
local auth_logger = logger:named("auth")
auth_logger:info("User login", {user_id = "user123"})
```

### Tree-sitter (treesitter)

```lua
-- Parse code
local tree = treesitter.parse("go", source_code)
local root = tree:root_node()

-- Node navigation
local child = root:child(0)
local kind = child:kind()
local text = child:text(source_code)

-- Queries
local query = treesitter.query("go", "(function_declaration) @func")
local matches = query:matches(root, source_code)

-- Walking tree
local cursor = tree:walk()
cursor:goto_first_child()
local node = cursor:current_node()
```

### Context (ctx)

```lua
local value, err = ctx.get("key")
local ok, err = ctx.set("key", "value")
```

### Environment (env)

```lua
local value, err = env.get("PATH")
local vars, err = env.get_all()
```

### Upstream (upstream)

```lua
local success = upstream.send("value")  -- Send to parent runtime
```

### WebSocket (websocket)

```lua
local client, err = websocket.connect("ws://example.com/socket")
client:send("message")
local ch = client:receive()
-- Messages: {type=TYPE_TEXT|TYPE_BINARY|TYPE_CLOSE, data="payload"}
```

## 7. Configuration System

### Process Configuration

```yaml
- name: process_name
  kind: process.lua
  meta:
    comment: "Long-running process"
    depends_on: [ "dependency1", "dependency2" ]
  source: file://source.lua
  method: run
  modules: [ "time", "json", "http_client" ]
  import:
    alias: "namespace:component"
```

### Process Service Configuration

```yaml
- name: background_service
  kind: process.service
  meta:
    comment: "Background service"
  process: process_name
  host: system:heap
  lifecycle:
    auto_start: true
    restart:
      initial_delay: 5s
      max_attempts: 3
    depends_on: [ "system:heap" ]
```

### HTTP Endpoint Configuration

```yaml
- name: api_endpoint
  kind: http.endpoint
  meta:
    comment: "API endpoint"
  method: GET
  path: /api/resource
  func: namespace:function_name
```

### HTTP Router Configuration

```yaml
- name: api_router
  kind: http.router
  meta:
    comment: "API router"
  base_path: /api
  routes:
    - path: /users
      method: GET
      func: namespace:list_users
    - path: /users/{id}
      method: GET
      func: namespace:get_user
```

### Function Configuration

```yaml
- name: function_name
  kind: function.lua
  meta:
    comment: "Function description"
  source: file://source.lua
  method: handler
  modules: [ "json", "uuid" ]
```

## 8. Common Programming Patterns

### Actor Model

```lua
local function run(args)
    local state = {counter = 0}
    local msgs = process.listen("command")
    
    while true do
        local cmd = msgs:receive()
        
        if cmd.action == "increment" then
            state.counter = state.counter + (cmd.value or 1)
            if cmd.reply_to then
                process.send(cmd.reply_to, "response", {count = state.counter})
            end
        elseif cmd.action == "get" then
            if cmd.reply_to then
                process.send(cmd.reply_to, "response", {count = state.counter})
            end
        elseif cmd.action == "reset" then
            state.counter = 0
        end
    end
end
```

### Worker Pool

```lua
local function run(args)
    local num_workers = args.workers or 10
    local task_channel = channel.new(100)
    local result_channel = channel.new(100)
    
    -- Spawn workers
    for i = 1, num_workers do
        coroutine.spawn(function()
            while true do
                local task, ok = task_channel:receive()
                if not ok then break end
                
                local result = process_task(task)
                result_channel:send({task_id = task.id, result = result})
            end
        end)
    end
    
    -- Listen for tasks
    local tasks = process.listen("task")
    local events = process.events()
    
    -- Process manager
    while true do
        local result = channel.select{
            tasks:case_receive(),
            result_channel:case_receive(),
            events:case_receive()
        }
        
        if result.channel == tasks then
            task_channel:send(result.value)
        elseif result.channel == result_channel then
            process.send(args.result_handler, "result", result.value)
        elseif result.channel == events then
            if result.value.event.kind == process.EVENT_CANCEL then
                task_channel:close()
                break
            end
        end
    end
    
    return {status = "shutdown"}
end
```

### Request-Response with Timeout

```lua
function handler(args)
    local inbox = func.inbox()
    
    -- Send request
    func.send(args.service_pid, "request", {
        id = uuid.v4(),
        action = "get_data",
        params = args.params,
        reply_to = func.pid()
    })
    
    -- Set up timeout
    local timeout = time.after(args.timeout or "5s")
    
    -- Wait for response with timeout
    local result = channel.select{
        inbox:case_receive(),
        timeout:case_receive()
    }
    
    if result.channel == timeout then
        return {status = "timeout", error = "Request timed out"}
    end
    
    local msg = result.value
    return {status = "success", data = msg.payload[1]}
end
```

### Event Streaming

```lua
function stream_handler()
    local req = http.request()
    local res = http.response()
    
    -- Set up SSE
    res:set_transfer(http.TRANSFER.SSE)
    
    -- Create event subscription
    local sub = subscribe.subscribe("events")
    
    -- Stream events to client
    coroutine.spawn(function()
        while true do
            local event, ok = sub:receive()
            if not ok then break end
            
            res:write_event({
                name = event.type,
                data = event.data
            })
            res:flush()
        end
    end)
    
    -- Check for client disconnect
    if req:connection_closed() then
        subscribe.unsubscribe(sub)
    end
}
```

## 9. Error Handling and Debugging

### Process Error Handling

```lua
local function run(args)
    -- Set up error handling for linked processes
    process.set_flags({
        trap_exits = true  -- Receive EXIT signals as messages
    })
    
    -- Monitor specific process
    local pid = process.spawn_monitored("namespace:name", "host", args)
    
    -- Listen for events
    local events = process.events()
    
    while true do
        local event = events:receive()
        
        if event.event.kind == process.EVENT_PROCESS_DOWN then
            -- Handle process failure
            local down_pid = event.event.pid
            local reason = event.event.reason
            
            logger:warn("Process down", {
                pid = down_pid,
                reason = reason
            })
            
            -- Restart process
            if reason ~= "normal" then
                process.spawn("namespace:name", "host", args)
            end
        end
    end
end
```

### Function Error Handling

```lua
function handler(args)
    -- Validate inputs
    if not args.required_param then
        return {
            status = "error",
            error = "Missing required parameter"
        }
    end
    
    -- Try operation with pcall
    local ok, result = pcall(function()
        return risky_operation(args.required_param)
    end)
    
    if not ok then
        logger:error("Operation failed", {
            error = result,
            param = args.required_param
        })
        
        return {
            status = "error",
            error = "Operation failed: " .. result
        }
    end
    
    return {
        status = "success",
        data = result
    }
end
```

### Debugging Channels

```lua
local function debug_channel(ch)
    local size = ch:_debug_size()
    local senders = ch:_debug_senders()
    local receivers = ch:_debug_receivers()
    
    logger:debug("Channel state", {
        buffer_size = size,
        blocked_senders = senders,
        blocked_receivers = receivers
    })
end
```

## 10. Resource Management with Unit of Work (UoW)

```lua
function handler()
    -- Get UoW from context
    local uw = uow.FromContext(ctx)
    
    -- Create resources with UoW context
    local file = fs.open("data.txt", "r")
    
    -- Register cleanup
    uw.Add(function() 
        return file:close()
    })
    
    -- Stream with automatic cleanup
    local stream = stream.NewStream(uw.Context(), file)
    uw.Add(function()
        return stream:close()
    })
    
    -- Use UoW context for cancellation
    coroutine.spawn(function()
        select {
            case <-uw.Context().Done():
                -- Clean up early
                return
            case data := <-processData(uw.Context()):
                -- Process data
        }
    })
    
    -- Resources automatically cleaned when UoW closes
    return result
end
```

## Key Principles to Remember

1. **Isolation**: All processes are memory-isolated with communication only through messages
2. **Message Formats**:

- Topic-specific channels receive direct values
- `process.inbox()` receives metadata-wrapped messages `{topic="topic", payload={value}}`

3. **Error Propagation**: Failures are captured and propagated via the supervision system
4. **Resource Cleanup**: Always use UoW for resource management to prevent leaks
5. **Non-blocking I/O**: Always use non-blocking patterns with channels and coroutines
6. **HTTP Streaming**: Supported via chunked transfer, SSE, and request body streaming
7. **Security**: Proper error handling, input validation, and resource limits are essential