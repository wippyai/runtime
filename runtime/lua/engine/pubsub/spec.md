# PubSub Layer Specification for Pony Coroutine VM

## Overview

The PubSub Layer implements a topic-based publish-subscribe messaging system for Pony processes. It allows coroutines to
communicate through named topics, with support for dynamic subscriptions and message delivery guarantees.

## Core Concepts

### Topics

- Named channels for message distribution
- Single active subscriber per topic
- Messages are delivered in order of publication
- Messages published before subscription are not delivered

### Subscriptions

- Dynamic subscription management
- Exclusive topic ownership
- Automatic cleanup on unsubscribe
- Channel-based message delivery

### Message Queue

- Ordered message delivery
- Non-blocking publication
- Buffered message handling
- Support for multiple message types

## Usage in Lua

### Creating Subscriptions

```lua
-- Subscribe to a topic
local subscriber = subscribe.subscribe("topic-name")

-- Receive messages
local message = subscriber:receive()

-- Unsubscribe when done
pubsub.unsubscribe(subscriber)
```

### Message Handling

```lua
-- Receive with status check
local message, ok = subscriber:receive()
if ok then
    -- Handle message
else
    -- Channel closed (unsubscribed)
end
```