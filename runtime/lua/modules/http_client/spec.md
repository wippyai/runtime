# Lua HTTP Module Specification

## Overview

The `http_client` module provides functions for performing HTTP requests in Lua. It supports various HTTP methods,
request
options (headers, cookies, body, query parameters, timeout, authentication), and batch requests. It also handles
response data, including headers, cookies, status code, URL, and body. Additionally, it supports streaming responses for
handling large data efficiently.

## Module Interface

### Module Loading

```lua
local http_client = require("http_client")
```

### Global Functions

#### `http_client.get(url: string, options: table)`

Sends an HTTP GET request.

Parameters:

- `url`: The URL to request.
- `options`: (Optional) A table of request options.

Returns:

- `response`: An `http.response` object (or nil on error).
- `error`: An error message string (or nil on success).

#### `http_client.post(url: string, options: table)`

Sends an HTTP POST request.

Parameters:

- `url`: The URL to request.
- `options`: (Optional) A table of request options.

Returns:

- `response`: An `http.response` object (or nil on error).
- `error`: An error message string (or nil on success).

#### `http_client.put(url: string, options: table)`

Sends an HTTP PUT request.

Parameters:

- `url`: The URL to request.
- `options`: (Optional) A table of request options.

Returns:

- `response`: An `http.response` object (or nil on error).
- `error`: An error message string (or nil on success).

#### `http_client.delete(url: string, options: table)`

Sends an HTTP DELETE request.

Parameters:

- `url`: The URL to request.
- `options`: (Optional) A table of request options.

Returns:

- `response`: An `http.response` object (or nil on error).
- `error`: An error message string (or nil on success).

#### `http_client.head(url: string, options: table)`

Sends an HTTP HEAD request.

Parameters:

- `url`: The URL to request.
- `options`: (Optional) A table of request options.

Returns:

- `response`: An `http.response` object (or nil on error).
- `error`: An error message string (or nil on success).

#### `http_client.patch(url: string, options: table)`

Sends an HTTP PATCH request.

Parameters:

- `url`: The URL to request.
- `options`: (Optional) A table of request options.

Returns:

- `response`: An `http.response` object (or nil on error).
- `error`: An error message string (or nil on success).

#### `http_client.request(method: string, url: string, options: table)`

Sends an HTTP request with the specified method.

Parameters:

- `method`: The HTTP method (e.g., "GET", "POST").
- `url`: The URL to request.
- `options`: (Optional) A table of request options.

Returns:

- `response`: An `http.response` object (or nil on error).
- `error`: An error message string (or nil on success).

#### `http_client.request_batch(requests: table)`

Sends multiple HTTP requests concurrently.

Parameters:

- `requests`: A table of request tables. Each request table contains:
    1. `method`: The HTTP method.
    2. `url`: The URL.
    3. `options`: (Optional) A table of request options.

Returns:

- `responses`: A table of `http.response` objects, indexed in the same order as the requests.
- `errors`: A table of error messages, indexed in the same order as the requests (or nil if no errors occurred).

#### `http_client.encode_uri(str: string)`

Encodes a string for use in a URL.

Parameters:

- `str`: The string to encode.

Returns:

- `encoded`: The encoded string.

#### `http_client.decode_uri(str: string)`

Decodes a URL-encoded string.

Parameters:

- `str`: The string to decode.

Returns:

- `decoded`: The decoded string (or nil on error).
- `error`: An error message string (or nil on success).

## Request Options

The `options` table can contain the following fields:

- `headers`: A table of HTTP headers (key-value pairs).
- `cookies`: A table of cookies (key-value pairs).
- `body`: The request body (string).
- `form`: Form data (string, will set `Content-Type` to `application/x-www-form-urlencoded`).
- `query`: The query string to append to the URL.
- `timeout`: The request timeout (number in seconds or string parsable by `time.ParseDuration`).
- `auth`: A table with `user` and `pass` fields for basic authentication.
- `stream`: A table for stream configuration for streaming requests:
    - `buffer_size`: (Optional) buffer size for stream `read()`

## HTTP Response Object

The `http_client.Response` object has the following fields:

- `headers`: A table of response headers (key-value pairs).
- `cookies`: A table of response cookies (key-value pairs).
- `status_code`: The HTTP status code (number).
- `url`: The final URL of the response (after redirects).
- `body`: The response body (string, nil if streaming is used).
- `body_size`: The size of the response body in bytes (-1 if streaming is used).
- `stream`: A `Stream` object for streamed responses (or nil if not a streaming response).

## Streamed Responses

- When the `stream` option is used, the response body will be `nil`, and `body_size` will be `-1`.
- The `stream` field will contain a `Stream` object that can be used to read the response body in chunks.
- The `read()` method of the `Stream` object will return chunks of data.
- The `close()` method of the `Stream` object should be called when finished to release resources.

## Error Handling

- Functions return an error message as the second return value if an error occurs.
- The `request_batch` function returns a table of error messages as the second return value.
- For streamed responses, errors during reading will be returned by the `read()` method of the `Stream` object.

## Behavior

- The module handles encoding of request bodies, headers, and cookies.
- It parses response headers and cookies.
- It supports concurrent requests with `request_batch`.
- It allows setting a timeout for requests.
- It supports basic authentication.
- For `request_batch`, it validates each request entry and builds requests with provided options.
- `request_batch` processes requests concurrently and returns results in order.

## Thread Safety

- The `http_client` module is not inherently thread-safe for concurrent access to the same `http_client.Response` object
  from multiple threads.
- Streamed responses using the same `Stream` object from multiple threads will lead to undefined behavior.

## Best Practices

- Check for errors after each function call.
- Use `request_batch` for efficient concurrent requests.
- Set appropriate timeouts for requests.
- Use the `stream` option for handling large responses efficiently.
- Close the stream when finished to release resources.
- Use `encode_uri` and `decode_uri` for proper URL handling.

## Example Usage

```lua
local http_client = require("http_client")

-- GET request
local response, err = http_client.get("https://api.example.com/data", {
  headers = {
    ["User-Agent"] = "Lua HTTP Client"
  },
  timeout = 5
})
if err then
  print("GET request failed:", err)
else
  print("Status:", response.status_code)
  print("Body:", response.body)
end

-- POST request with form data
local response, err = http_client.post("https://api.example.com/submit", {
  form = "name=John+Doe&age=30"
})
if err then
  print("POST request failed:", err)
else
  print("Response:", response.body)
end

-- Batch requests
local requests = {
  { "GET", "https://api.example.com/users" },
  { "GET", "https://api.example.com/posts", { timeout = 2 } }
}
local responses, errors = http_client.request_batch(requests)
for i, res in ipairs(responses) do
  if res then
    print("Request", i, "Status:", res.status_code)
  else
    print("Request", i, "Error:", errors[i])
  end
end

-- Streaming response
local response, err = http_client.get("https://api.example.com/largefile", { stream = { buffer_size = 4096 } })
if err then
  print("Streaming request failed:", err)
else
  local stream = response.stream
  for chunk in stream() do
    -- Process each chunk
    print("Chunk:", chunk)
  end
  stream:close()
end
```
