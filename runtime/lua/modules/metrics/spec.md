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
2. Error message (string on failure, `nil` on success)

```lua
local ok, err = metrics.counter_inc("my_counter")
if err then
    -- handle error
end
```

### Global Functions

#### metrics.counter_inc(name: string, labels?: table)

Increments a counter by 1.

Parameters:

- `name`: The metric name
- `labels`: (Optional) Table of string key-value pairs for metric dimensions

Returns:

- `boolean, error`: `true` on success, or `nil` and error message on failure

Errors:

- "metrics collector not available"

#### metrics.counter_add(name: string, value: number, labels?: table)

Adds a value to a counter.

Parameters:

- `name`: The metric name
- `value`: The value to add (must be positive)
- `labels`: (Optional) Table of string key-value pairs for metric dimensions

Returns:

- `boolean, error`: `true` on success, or `nil` and error message on failure

Errors:

- "metrics collector not available"

#### metrics.gauge_set(name: string, value: number, labels?: table)

Sets a gauge to a specific value.

Parameters:

- `name`: The metric name
- `value`: The value to set
- `labels`: (Optional) Table of string key-value pairs for metric dimensions

Returns:

- `boolean, error`: `true` on success, or `nil` and error message on failure

Errors:

- "metrics collector not available"

#### metrics.gauge_inc(name: string, labels?: table)

Increments a gauge by 1.

Parameters:

- `name`: The metric name
- `labels`: (Optional) Table of string key-value pairs for metric dimensions

Returns:

- `boolean, error`: `true` on success, or `nil` and error message on failure

Errors:

- "metrics collector not available"

#### metrics.gauge_dec(name: string, labels?: table)

Decrements a gauge by 1.

Parameters:

- `name`: The metric name
- `labels`: (Optional) Table of string key-value pairs for metric dimensions

Returns:

- `boolean, error`: `true` on success, or `nil` and error message on failure

Errors:

- "metrics collector not available"

#### metrics.histogram(name: string, value: number, labels?: table)

Records a value in a histogram.

Parameters:

- `name`: The metric name
- `value`: The observed value
- `labels`: (Optional) Table of string key-value pairs for metric dimensions

Returns:

- `boolean, error`: `true` on success, or `nil` and error message on failure

Errors:

- "metrics collector not available"

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
- Non-string values are ignored

## Thread Safety

- All functions are thread-safe
- Metrics are collected asynchronously via buffered channel
- No blocking on the hot path

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

## Configuration

The metrics module requires the metrics collector to be enabled in boot configuration:

```yaml
metrics:
  buffer:
    size: 10000
  interceptor:
    enabled: true
```

Export backends are configured separately:

```yaml
# OTEL export
otel:
  enabled: true
  metrics_enabled: true

# Prometheus export
prometheus:
  enabled: true
```
