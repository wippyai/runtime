# Lua Process API Module Specification

## Overview

The `process_api` module provides relay-based inbox and channel functionality for long-running processes. It extends the base process API with process-specific communication and configuration features.

## Module Interface

### Module Loading

```lua
local process = require("process_api")
```

This module inherits all functions from the `process` module and adds additional process-specific capabilities.

### Functions

#### process.inbox()

Returns a channel for receiving messages sent to this process.

Returns:
- `channel`: Inbox channel that receives incoming messages

Notes:
- Messages are delivered as packages with routing information
- Channel remains open for the lifetime of the process

#### process.events()

Returns a channel for receiving event notifications.

Returns:
- `channel`: Events channel for system and application events

Notes:
- Events include registry changes, system notifications, and state updates
- Automatically subscribes to relevant events

#### process.listen(pattern: string)

Subscribes to messages matching a specific pattern.

Parameters:
- `pattern`: Pattern string for message filtering

Returns:
- `success`: Boolean indicating subscription success

#### process.unlisten(channel: userdata)

Unsubscribes a channel from receiving messages.

Parameters:
- `channel`: Channel userdata to unsubscribe

Returns:
- `success`: Boolean indicating unsubscription success

#### process.get_options()

Retrieves the current process configuration options.

Returns:
- `options`: Table containing process configuration
- `error`: Error message (or nil on success)

#### process.set_options(options: table)

Updates process configuration options.

Parameters:
- `options`: Table with configuration settings

Returns:
- `success`: Boolean indicating whether options were set
- `error`: Error message (or nil on success)

## Example Usage

```lua
local process = require("process_api")

-- Get and modify process options
local opts, err = process.get_options()
if not err then
  opts.buffer_size = 100
  process.set_options(opts)
end

-- Set up message routing
process.listen("jobs.*")
process.listen("notifications.high")

-- Receive messages
local inbox = process.inbox()
for msg in inbox do
  print("Processing message:", msg)
  -- Handle message
end

-- Subscribe to events
local events = process.events()
for event in events do
  if event.kind == "registry.update" then
    print("Registry updated")
  end
end

-- Clean up (unsubscribe the inbox channel)
process.unlisten(inbox)
```

## Notes

- This module is designed for long-running process lifecycles
- Inherits all process API functions (pid, send, spawn, etc.)
- Subscriptions persist for the lifetime of the process
- Options control message buffering and routing behavior
- Inbox and events are persistent channels
