# Events Module Specification

## Overview

The Events module provides access to the Pony event bus system through a simple, channel-based API. It enables Lua processes to subscribe to specific event patterns, receive events through channels, and send events to the bus, supporting concurrent event processing with coroutines.

## Module Interface

### Loading the Module

```lua
local events = require("events")
```

### Methods

#### subscribe(system, [kind])

Creates a subscription to events matching the system and optional kind pattern.

```lua
-- Subscribe to all events in the "users" system
local subscription = events.subscribe("users")

-- Subscribe to specific event kinds with pattern matching
local subscription = events.subscribe("users", "*.created")
```

Parameters:
- `system` (string): System identifier or pattern (e.g., "users", "auth", "*")
- `kind` (string, optional): Event kind pattern (e.g., "created", "user.*", "*")

Returns:
- Subscription object on success
- `nil, error_message` on failure

#### send(system, kind, path, [data])

Sends an event to the event bus.

```lua
-- Send event without data
events.send("users", "user.created", "users/123")

-- Send event with data
local success = events.send("users", "user.updated", "users/123", {
    name = "John Doe",
    email = "john@example.com",
    age = 30
})
```

Parameters:
- `system` (string): System identifier (e.g., "users", "auth")
- `kind` (string): Event kind (e.g., "user.created", "user.updated")
- `path` (string): Event path identifier (e.g., "users/123", "auth/sessions/456")
- `data` (any, optional): Event payload data (can be any Lua type)

Returns:
- `true` on success
- `false, error_message` on failure

## Subscription Object

The subscription object returned by `events.subscribe()` has the following methods:

#### channel()

Returns the channel that receives matching events.

```lua
local ch = subscription:channel()
```

Returns:
- Channel object that receives event tables

#### close()

Closes the subscription, removing it from the event bus and stopping event delivery.

```lua
subscription:close()
```

Returns:
- `true` on success

## Event Format

Events received through subscription channels are Lua tables with the following fields:

```lua
{
    system = "users",       -- System identifier (string)
    kind = "user.created",  -- Event kind (string)
    path = "users/123",     -- Optional path identifier (string)
    data = { ... }          -- Event payload (any type)
}
```

## Resource Management

- Subscriptions are automatically closed when the unit of work (UoW) that created them ends
- Explicitly calling `subscription:close()` is recommended when a subscription is no longer needed
- All event resources are properly cleaned up when subscriptions are closed

## Integration with Channels

Events module is designed to work seamlessly with the Pony channel system. For performance, it's recommended to store channel references in variables rather than calling `subscription:channel()` repeatedly:

```lua
-- Get channel once and reuse
local ch = subscription:channel()

-- Receive events one at a time
local evt, ok = ch:receive()

-- Use with channel.select for concurrent operations
local other_ch = other_subscription:channel()
local result = channel.select{
    ch:case_receive(),
    other_ch:case_receive()
}
```

## Error Handling

Most operations can return errors:

```lua
-- Handle subscription errors
local subscription, err = events.subscribe("users", "*.created")
if not subscription then
    error("Failed to subscribe: " .. err)
end

-- Handle send errors
local success, err = events.send("users", "user.created", "users/123", data)
if not success then
    error("Failed to send event: " .. err)
end

-- Handle channel errors
local evt, ok = subscription:channel():receive()
if not ok then
    -- Channel closed or error occurred
end
```

## Usage Examples

### Basic Event Subscription

```lua
local events = require("events")

-- Subscribe to all auth events
local subscription = events.subscribe("auth", "*")

-- Process events in a loop
while true do
    local evt, ok = subscription:channel():receive()
    if not ok then
        print("Subscription channel closed")
        break
    end
    
    print("Received event:", evt.system, evt.kind)
    
    -- Process the event based on its kind
    if evt.kind == "user.login" then
        handle_login(evt.data)
    elseif evt.kind == "user.logout" then
        handle_logout(evt.data)
    end
end

-- Close subscription when done
subscription:close()
```

### Sending Events

```lua
local events = require("events")

-- Send a simple event
events.send("users", "user.created", "users/123")

-- Send event with data
local user_data = {
    user_id = 123,
    name = "Alice Smith",
    email = "alice@example.com",
    created_at = os.time()
}

local success = events.send("users", "user.created", "users/123", user_data)
if not success then
    error("Failed to send user created event")
end

-- Send notification event
events.send("notifications", "email.queued", "emails/456", {
    recipient = "alice@example.com",
    template = "welcome",
    priority = "high"
})
```

### Event Processing with Coroutines

```lua
local events = require("events")

-- Create multiple subscriptions
local user_events = events.subscribe("users")
local system_events = events.subscribe("system")

-- Get channels once
local user_ch = user_events:channel()
local system_ch = system_events:channel()

-- Process users events in a separate coroutine
coroutine.spawn(function()
    while true do
        local evt, ok = user_ch:receive()
        if not ok then break end
        process_user_event(evt)
    end
end)

-- Process system events in another coroutine
coroutine.spawn(function()
    while true do
        local evt, ok = system_ch:receive()
        if not ok then break end
        process_system_event(evt)
    end
end)

-- Continue with other operations in the main coroutine
do_something_else()
```

### Event Filtering with Pattern Matching

```lua
local events = require("events")

-- Subscribe to specific event patterns
local created_events = events.subscribe("*", "*.created")
local deleted_events = events.subscribe("*", "*.deleted")
local user_events = events.subscribe("users", "*")

-- Get channels once and reuse
local created_ch = created_events:channel()
local deleted_ch = deleted_events:channel()
local user_ch = user_events:channel()

-- Process events from multiple subscriptions with select
while true do
    local result = channel.select{
        created_ch:case_receive(),
        deleted_ch:case_receive(),
        user_ch:case_receive()
    }
    
    if result.channel == created_ch then
        handle_creation(result.value)
    elseif result.channel == deleted_ch then
        handle_deletion(result.value)
    elseif result.channel == user_ch then
        handle_user_event(result.value)
    end
end
```

### Request-Response Pattern with Events

```lua
local events = require("events")

-- Function to handle user creation requests
function handle_user_creation_request(request_event)
    local user_data = request_event.data
    
    -- Validate user data
    if not user_data.email or not user_data.name then
        -- Send error response
        events.send("users", "user.creation.failed", request_event.path, {
            error = "Missing required fields",
            request_id = user_data.request_id
        })
        return
    end
    
    -- Create user in database
    local user_id = create_user_in_db(user_data)
    
    -- Send success response
    events.send("users", "user.created", "users/" .. user_id, {
        user_id = user_id,
        name = user_data.name,
        email = user_data.email,
        request_id = user_data.request_id
    })
end

-- Subscribe to user creation requests
local subscription = events.subscribe("users", "user.creation.requested")

-- Process requests
while true do
    local evt, ok = subscription:channel():receive()
    if not ok then break end
    
    coroutine.spawn(function()
        handle_user_creation_request(evt)
    end)
end
```

### Helper Function for Event Handling

```lua
local events = require("events")

-- Helper function to subscribe with a handler
function subscribe_with_handler(system, kind, handler)
    local subscription = events.subscribe(system, kind)
    local ch = subscription:channel()
    
    -- Process events in a background coroutine
    coroutine.spawn(function()
        while true do
            local evt, ok = ch:receive()
            if not ok then break end
            
            -- Call the handler with the event
            handler(evt)
        end
    end)
    
    return subscription
end

-- Usage example
local auth_sub = subscribe_with_handler("auth", "user.*", function(evt)
    if evt.kind == "user.login" then
        log_login(evt.data.user_id, evt.data.timestamp)
        
        -- Send welcome notification
        events.send("notifications", "login.welcome", evt.path, {
            user_id = evt.data.user_id,
            template = "login_welcome"
        })
    elseif evt.kind == "user.logout" then
        log_logout(evt.data.user_id, evt.data.timestamp)
    end
end)

-- Later: close subscription when done
auth_sub:close()
```

### Event Workflow Processing

```lua
local events = require("events")

-- Function to process order workflow
function process_order_workflow()
    local order_events = events.subscribe("orders", "*")
    local payment_events = events.subscribe("payments", "*")
    
    -- Get channels once and reuse
    local order_ch = order_events:channel()
    local payment_ch = payment_events:channel()
    
    while true do
        local result = channel.select{
            order_ch:case_receive(),
            payment_ch:case_receive()
        }
        
        if result.channel == order_ch then
            local evt = result.value
            
            if evt.kind == "order.created" then
                -- Send payment request
                events.send("payments", "payment.requested", evt.path, {
                    order_id = evt.data.order_id,
                    amount = evt.data.total,
                    customer_id = evt.data.customer_id
                })
            end
            
        elseif result.channel == payment_ch then
            local evt = result.value
            
            if evt.kind == "payment.completed" then
                -- Send fulfillment request
                events.send("fulfillment", "order.fulfill", evt.path, {
                    order_id = evt.data.order_id,
                    items = evt.data.items
                })
                
                -- Send confirmation email
                events.send("notifications", "email.queued", evt.path, {
                    template = "order_confirmation",
                    recipient = evt.data.customer_email,
                    order_id = evt.data.order_id
                })
            end
        end
    end
end

-- Start the workflow processor
coroutine.spawn(process_order_workflow)
```

### Timed Events with Integration

```lua
local events = require("events")
local time = require("time")

-- Subscribe to events
local subscription = events.subscribe("alerts", "*")
local sub_ch = subscription:channel()

-- Create a timer for periodic checks
local timer = time.after("5s")

-- Wait for either event or timeout
local result = channel.select{
    sub_ch:case_receive(),
    timer:case_receive()
}

if result.channel == sub_ch then
    -- Process event
    handle_alert(result.value)
else
    -- Timeout occurred - send heartbeat event
    events.send("system", "heartbeat", "monitoring/health", {
        timestamp = os.time(),
        status = "healthy"
    })
end

-- Clean up
subscription:close()
```

## Best Practices

1. **Always close subscriptions** when no longer needed
2. **Use pattern matching** for efficient event filtering
3. **Handle channel closure** properly in receive loops
4. **Spawn coroutines** for concurrent event processing
5. **Use `channel.select`** to handle multiple event sources
6. **Keep event handlers small and focused** to process events efficiently
7. **Include meaningful data** in events to enable proper processing
8. **Use consistent naming conventions** for systems, kinds, and paths
9. **Handle send failures** appropriately for critical events
10. **Design events for forward compatibility** by using structured data
11. **Store channel references in variables** instead of calling `subscription:channel()` repeatedly for better performance