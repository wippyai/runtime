<!-- SPDX-License-Identifier: MPL-2.0 -->

# metrics

Recording counters, gauges, and histograms. IO, nondeterministic.

## Loading

```lua
local metrics = require("metrics")
```

## Functions

### counter_inc(name: string, labels?: table) → boolean, error

Increments a counter by 1.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| name | string | yes | - | Metric name |
| labels | table | no | nil | String key-value pairs |

**labels fields:**

Labels are optional. Only string keys and string values are used; non-string values are silently ignored.

**Returns:**
- Success: `true, nil`
- Error: `nil, error` - error is structured (has `:kind()`, `:message()`)

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| collector not available | errors.INTERNAL | no |

### counter_add(name: string, value: number, labels?: table) → boolean, error

Adds a value to a counter.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| name | string | yes | - | Metric name |
| value | number | yes | - | Value to add (should be positive) |
| labels | table | no | nil | String key-value pairs |

**Returns:**
- Success: `true, nil`
- Error: `nil, error` - error is structured (has `:kind()`, `:message()`)

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| collector not available | errors.INTERNAL | no |

### gauge_set(name: string, value: number, labels?: table) → boolean, error

Sets a gauge to a specific value.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| name | string | yes | - | Metric name |
| value | number | yes | - | Value to set |
| labels | table | no | nil | String key-value pairs |

**Returns:**
- Success: `true, nil`
- Error: `nil, error` - error is structured (has `:kind()`, `:message()`)

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| collector not available | errors.INTERNAL | no |

### gauge_inc(name: string, labels?: table) → boolean, error

Increments a gauge by 1.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| name | string | yes | - | Metric name |
| labels | table | no | nil | String key-value pairs |

**Returns:**
- Success: `true, nil`
- Error: `nil, error` - error is structured (has `:kind()`, `:message()`)

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| collector not available | errors.INTERNAL | no |

### gauge_dec(name: string, labels?: table) → boolean, error

Decrements a gauge by 1.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| name | string | yes | - | Metric name |
| labels | table | no | nil | String key-value pairs |

**Returns:**
- Success: `true, nil`
- Error: `nil, error` - error is structured (has `:kind()`, `:message()`)

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| collector not available | errors.INTERNAL | no |

### histogram(name: string, value: number, labels?: table) → boolean, error

Records a value in a histogram.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| name | string | yes | - | Metric name |
| value | number | yes | - | Observed value |
| labels | table | no | nil | String key-value pairs |

**Returns:**
- Success: `true, nil`
- Error: `nil, error` - error is structured (has `:kind()`, `:message()`)

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| collector not available | errors.INTERNAL | no |

## Errors

This module returns structured errors. Check kind with `errors.*` constants:

```lua
local ok, err = metrics.counter_inc("my_counter")
if err then
    if err:kind() == errors.INTERNAL then
        -- collector not available
    end
end
```

**Possible kinds:** `errors.INTERNAL`

## Example

```lua
local metrics = require("metrics")

-- Count requests
local ok, err = metrics.counter_inc("app.requests.total", {
    endpoint = "/api/users"
})
if err then error(err) end

-- Track active connections
metrics.gauge_inc("app.connections.active")
-- ... later
metrics.gauge_dec("app.connections.active")

-- Set queue depth
metrics.gauge_set("app.queue.depth", 42, {
    queue = "emails"
})

-- Record response time
metrics.histogram("app.request.duration", 0.123, {
    endpoint = "/api/users"
})

-- Batch processing
metrics.counter_add("app.records.processed", 100, {
    batch_id = "batch-123"
})
```
