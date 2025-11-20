# Lua Function API Module Specification

## Overview

The `function_api` module provides inbox handling and event subscription capabilities for short-lived functions. It extends the base process API with function-specific communication features.

## Module Interface

### Module Loading

```lua
local fn = require("function_api")
```

This module inherits all functions from the `process` module and adds additional function-specific capabilities.

### Functions

#### fn.inbox()

Returns a channel for receiving messages sent to this function's process.

Returns:
- `channel`: Inbox channel that receives incoming messages

Notes:
- Lazily initializes the inbox on first call
- Messages are delivered as packages with routing information

#### fn.events()

Returns a channel for receiving event notifications.

Returns:
- `channel`: Events channel for system and application events

Notes:
- Subscribes to events on first call
- Events include system notifications and state changes

#### fn.listen(pattern: string)

Subscribes to messages matching a specific pattern.

Parameters:
- `pattern`: Pattern string for message filtering

Returns:
- `success`: Boolean indicating subscription success

#### fn.unlisten(channel: userdata)

Unsubscribes a channel from receiving messages.

Parameters:
- `channel`: Channel userdata to unsubscribe

Returns:
- `success`: Boolean indicating unsubscription success

## Example Usage

```lua
local fn = require("function_api")

-- Get inbox for receiving messages
local inbox = fn.inbox()

-- Listen for specific message types
fn.listen("user.*")
fn.listen("system.alerts")

-- Receive messages
for msg in inbox do
  print("Received:", msg)
  -- Process message
end

-- Stop listening when done (unsubscribe the inbox channel)
fn.unlisten(inbox)

-- Get events channel
local events = fn.events()
for event in events do
  print("Event:", event.kind)
end
```

## Notes

- This module is designed for short-lived function executions
- Inherits all process API functions (pid, send, spawn, etc.)
- Subscriptions are automatically cleaned up when the function exits
- Inbox and events channels are lazily initialized on first access
