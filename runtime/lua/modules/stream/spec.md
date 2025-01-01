# Lua Stream Module Specification

## Overview

The `Stream` module provides a Lua interface for reading data in chunks from a stream. It supports configurable buffer
sizes, error handling, and iteration over the stream's contents.

## Module Interface

### Module Loading

```lua
local Stream = require("Stream") -- Corrected module name
```

### Stream Object

The module primarily works with `Stream` objects, which represent a readable data stream.

### Stream Creation

`Stream` objects are not created directly from Lua. They are expected to be created and passed from an external
environment.

## Methods

### Stream:read()

Reads a chunk of data from the stream.

Returns:

- `string`: The chunk of data read, or `nil` if the end of the stream is reached.
- `string`: An error message, or `nil` if no error occurred.

### Stream:close()

Closes the stream.

Returns:

- `string`: An error message, or `nil` if no error occurred.

### Stream:bytes_read()

Returns the total number of bytes read from the stream.

Returns:

- `number`: The total number of bytes read.

### Stream:__call()

Enables iteration over the stream using a `for` loop.

Returns:

- `function`: An iterator function that returns the next chunk of data on each call.

## Iteration

The `Stream` object can be used in a `for` loop to iterate over the stream's contents:

```lua
for chunk in test_stream() do
  -- Process the chunk of data
end
```

## Error Handling

- Methods return an error message as the second return value if an error occurs.
- The `read()` method returns `nil` as the first value to indicate the end of the stream.
- Attempting to read from a closed stream will return an error.

## Behavior

- The `read()` method reads a chunk of data from the underlying stream. The chunk size is determined by the configured
  buffer size.
- The `close()` method closes the underlying stream.
- The `bytes_read()` method returns the cumulative number of bytes read from the stream.
- The iterator function returned by `__call()` allows iterating over the stream in a `for` loop. Each iteration yields
  the next chunk of data until the end of the stream is reached.
- If the iterator function encounters an error, it terminates the iteration and returns `nil` in the next iteration.

## Thread Safety

- The `Stream` module does not provide explicit thread safety guarantees. Concurrent access to the same `Stream` object
  from multiple threads may lead to undefined behavior.

## Best Practices

- Check for errors after each method call, especially `read()` and `close()`.
- Use the `for` loop iteration pattern to process streams in a concise and idiomatic way.
- Close the stream when finished to release resources.
- Be mindful of the configured buffer size, as it can affect performance and memory usage.
- Avoid concurrent access to the same `Stream` object from multiple threads.

## Example Usage

```lua
-- Assuming a Stream object named 'test_stream' is available

-- Read and print all chunks from the stream
while true do
  local chunk, err = test_stream:read()
  if err then
    error("Error reading stream: " .. err)
  end
  if not chunk then
    break
  end
  print("Chunk:", chunk)
end

-- Get the total number of bytes read
local totalBytes = test_stream:bytes_read()
print("Total bytes read:", totalBytes)

-- Close the stream
local err = test_stream:close()
if err then
  error("Error closing stream: " .. err)
end

-- Iterate over the stream using a for loop
for chunk in test_stream() do
  print("Chunk (iter):", chunk)
end
```
