# Lua Metrics Module Specification

## Overview

The `metrics` module provides a Lua interface for recording application metrics. It supports three metric types: counters, gauges, and histograms. Metrics are collected asynchronously and exported to configured backends (OTEL, Prometheus).

## Module Interface

### Module Loading

```lua
local metrics = require("metrics")
```

### Error Handling

All functions return two values:

1. Result (boolean `true` on success, `nil` on failure)
2. Error (structured error on failure, `nil` on success)

```lua
local ok, err = metrics.counter_inc("my_counter")
if err then
    print(err:message())
    print(err:kind())  -- errors.INTERNAL
end
```

### Functions

#### metrics.counter_inc(name: string, labels?: table)

Increments a counter by 1.

Parameters:
- `name`: The metric name
- `labels`: (Optional) Table of string key-value pairs

Returns:
- `boolean, error`: `true` on success, or `nil` and structured error on failure

#### metrics.counter_add(name: string, value: number, labels?: table)

Adds a value to a counter.

Parameters:
- `name`: The metric name
- `value`: The value to add (should be positive)
- `labels`: (Optional) Table of string key-value pairs

Returns:
- `boolean, error`: `true` on success, or `nil` and structured error on failure

#### metrics.gauge_set(name: string, value: number, labels?: table)

Sets a gauge to a specific value.

Parameters:
- `name`: The metric name
- `value`: The value to set
- `labels`: (Optional) Table of string key-value pairs

Returns:
- `boolean, error`: `true` on success, or `nil` and structured error on failure

#### metrics.gauge_inc(name: string, labels?: table)

Increments a gauge by 1.

Parameters:
- `name`: The metric name
- `labels`: (Optional) Table of string key-value pairs

Returns:
- `boolean, error`: `true` on success, or `nil` and structured error on failure

#### metrics.gauge_dec(name: string, labels?: table)

Decrements a gauge by 1.

Parameters:
- `name`: The metric name
- `labels`: (Optional) Table of string key-value pairs

Returns:
- `boolean, error`: `true` on success, or `nil` and structured error on failure

#### metrics.histogram(name: string, value: number, labels?: table)

Records a value in a histogram.

Parameters:
- `name`: The metric name
- `value`: The observed value
- `labels`: (Optional) Table of string key-value pairs

Returns:
- `boolean, error`: `true` on success, or `nil` and structured error on failure

## Metric Types

### Counters

Monotonically increasing values. Use for counting events.

- `counter_inc`: Increment by 1
- `counter_add`: Add arbitrary positive value

### Gauges

Values that can go up or down. Use for current state.

- `gauge_set`: Set to absolute value
- `gauge_inc`: Increment by 1
- `gauge_dec`: Decrement by 1

### Histograms

Distribution of values. Use for latencies and sizes.

- `histogram`: Record an observation

## Labels

Labels are optional string key-value pairs that add dimensions to metrics:

```lua
metrics.counter_inc("http_requests", {
    method = "GET",
    path = "/api/users",
    status = "200"
})
```

- Label keys must be strings
- Label values must be strings
- Non-string values are silently ignored

## Error Handling

### Error Types

**No Collector Available:**

```lua
local ok, err = metrics.counter_inc("test")
if err then
    print(err:kind())  -- errors.INTERNAL
    print(err:message())  -- "metrics collector not available"
end
```

### Error Kind Comparison

Always use `errors.*` constants:

```lua
if err:kind() == errors.INTERNAL then
    -- collector not available
end
```

## Example Usage

```lua
local metrics = require("metrics")

-- Count requests
metrics.counter_inc("app.requests.total", {
    endpoint = "/api/users"
})

-- Track active connections
metrics.gauge_inc("app.connections.active")
-- ... later
metrics.gauge_dec("app.connections.active")

-- Set queue depth
metrics.gauge_set("app.queue.depth", 42, {
    queue = "emails"
})

-- Record response time
local start = os.clock()
-- ... do work
local duration = os.clock() - start
metrics.histogram("app.request.duration", duration, {
    endpoint = "/api/users"
})

-- Batch processing metrics
metrics.counter_add("app.records.processed", 100, {
    batch_id = "batch-123"
})
```

## Thread Safety

- All functions are thread-safe
- Metrics are collected asynchronously via buffered channel
- No blocking on the hot path

## Module Classification

- **Class**: `io`, `nondeterministic`

## Go Implementation

```go
var Module = &luaapi.ModuleDef{
    Name:        "metrics",
    Description: "Counters, gauges, and histograms",
    Class:       []string{luaapi.ClassIO, luaapi.ClassNondeterministic},
    Build: func() (*lua.LTable, []luaapi.YieldType) {
        return moduleTable, nil
    },
}
```
