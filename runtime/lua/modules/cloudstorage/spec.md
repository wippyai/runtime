<!-- SPDX-License-Identifier: MPL-2.0 -->

# cloudstorage

Cloud storage operations for S3, GCS, and other providers. Storage, network, IO.

## Loading

```lua
local cloudstorage = require("cloudstorage")
```

## Functions

### get(id: string) → Storage, error

Acquires a cloud storage resource by ID.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| id | string | yes | - | Resource ID (format: "namespace:name") |

**Returns:**
- Success: `Storage` - storage connection object
- Error: `nil, error` - error is structured (has `:kind()`, `:message()`)

**Errors (structured):**

| Condition | Kind | Retryable |
|-----------|------|-----------|
| id is empty string | errors.INVALID | no |
| resource not found | errors.NOT_FOUND | no |
| resource is not cloud storage | errors.INVALID | no |
| resource store/registry not found | errors.INTERNAL | no |
| permission denied | - | no (raises error) |

**Notes:**
- Resource is automatically released when script completes
- Call `storage:release()` to release early
- Security policy enforced via `cloudstorage.get` permission

## Types

### Storage

Returned by `cloudstorage.get()`. Provides methods for cloud storage operations.

| Method | Signature | Returns | Notes |
|--------|-----------|---------|-------|
| list_objects | (options?: table) | table, error | Lists objects with optional filtering |
| download_object | (key: string, writer: io.Writer, options?: table) | boolean, error | Downloads object to writer |
| upload_object | (key: string, content: string \| io.Reader) | boolean, error | Uploads object from string or reader |
| delete_objects | (keys: string[]) | boolean, error | Deletes multiple objects |
| presigned_get_url | (key: string, options?: table) | string, error | Generates presigned download URL |
| presigned_put_url | (key: string, options?: table) | string, error | Generates presigned upload URL |
| release | () | boolean | Releases storage resource |

#### storage:list_objects(options?: table) → table, error

Lists objects in storage with optional filtering.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| options | table | no | nil | Filtering and pagination options |

**options fields:**

| Field | Type | Default | Notes |
|-------|------|---------|-------|
| prefix | string | "" | Filter objects starting with prefix |
| max_keys | integer | 0 | Maximum objects to return (0 = unlimited) |
| continuation_token | string | "" | Token for pagination |

**Returns:**
- Success: `table` - result table with fields below
- Error: `nil, error` - error is structured

**Result table:**

| Field | Type | Notes |
|-------|------|-------|
| objects | table[] | Array of object metadata tables |
| is_truncated | boolean | True if more results available |
| next_continuation_token | string | Token for next page (empty if !is_truncated) |

**Object metadata table:**

| Field | Type | Notes |
|-------|------|-------|
| key | string | Object key/path |
| size | integer | Object size in bytes |
| content_type | string | MIME type |
| etag | string | Entity tag |

**Errors (structured):**

| Condition | Kind |
|-----------|------|
| storage released | errors.INVALID |
| operation failed | errors.INTERNAL |

**Yields:** until operation completes

```lua
local result, err = storage:list_objects({ prefix = "photos/", max_keys = 100 })
if err then error(err) end

for _, obj in ipairs(result.objects) do
    print(obj.key, obj.size, obj.content_type)
end

if result.is_truncated then
    local next_result = storage:list_objects({
        prefix = "photos/",
        continuation_token = result.next_continuation_token
    })
end
```

#### storage:download_object(key: string, writer: io.Writer, options?: table) → boolean, error

Downloads an object to an io.Writer (typically fs.File).

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| key | string | yes | - | Object key to download |
| writer | io.Writer | yes | - | Destination writer (e.g., fs.File) |
| options | table | no | nil | Download options |

**options fields:**

| Field | Type | Default | Notes |
|-------|------|---------|-------|
| range | string | "" | Byte range (e.g., "bytes=0-1023" for first 1KB) |

**Returns:**
- Success: `true`
- Error: `nil, error` - error is structured

**Errors (structured):**

| Condition | Kind |
|-----------|------|
| key empty | errors.INVALID |
| storage released | errors.INVALID |
| writer not io.Writer | errors.INVALID |
| object not found | errors.NOT_FOUND |
| operation failed | errors.INTERNAL |

**Yields:** until download completes

**Notes:**
- Writer must implement io.Writer interface (e.g., fs.File opened with "w" or "a" mode)
- Content is written to writer as it's downloaded

```lua
local fs = require("fs")
local vol, _ = fs.get("app:temp")
local file, _ = vol:open("/downloaded.txt", "w")
local ok, err = storage:download_object("data/file.txt", file)
file:close()
if err then error(err) end
```

#### storage:upload_object(key: string, content: string | io.Reader) → boolean, error

Uploads an object from string or io.Reader.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| key | string | yes | - | Object key/path |
| content | string \| io.Reader | yes | - | Content as string or reader (e.g., fs.File) |

**Returns:**
- Success: `true`
- Error: `nil, error` - error is structured

**Errors (structured):**

| Condition | Kind |
|-----------|------|
| key empty | errors.INVALID |
| content nil | errors.INVALID |
| storage released | errors.INVALID |
| operation failed | errors.INTERNAL |

**Yields:** until upload completes

**Notes:**
- String content is converted to bytes automatically
- io.Reader content (e.g., fs.File) should be opened in read mode ("r")
- File is read completely during upload

```lua
-- Upload string
storage:upload_object("data/hello.txt", "Hello, World!")

-- Upload from file
local fs = require("fs")
local vol, _ = fs.get("app:temp")
local file, _ = vol:open("/local.txt", "r")
storage:upload_object("data/uploaded.txt", file)
file:close()
```

#### storage:delete_objects(keys: string[]) → boolean, error

Deletes multiple objects.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| keys | string[] | yes | - | Array of object keys to delete |

**Returns:**
- Success: `true`
- Error: `nil, error` - error is structured

**Errors (structured):**

| Condition | Kind |
|-----------|------|
| storage released | errors.INVALID |
| operation failed | errors.INTERNAL |

**Yields:** until deletion completes

**Notes:**
- Deleting non-existent objects does not cause error
- All deletions are attempted even if some fail

```lua
storage:delete_objects({"file1.txt", "file2.txt", "dir/file3.txt"})
```

#### storage:presigned_get_url(key: string, options?: table) → string, error

Generates a presigned URL for downloading an object without credentials.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| key | string | yes | - | Object key |
| options | table | no | nil | URL options |

**options fields:**

| Field | Type | Default | Notes |
|-------|------|---------|-------|
| expiration | integer | 3600 | Seconds until URL expires (default 1 hour) |

**Returns:**
- Success: `string` - presigned URL
- Error: `nil, error` - error is structured

**Errors (structured):**

| Condition | Kind |
|-----------|------|
| key empty | errors.INVALID |
| storage released | errors.INVALID |
| operation failed | errors.INTERNAL |

**Yields:** until URL generation completes

**Notes:**
- URL is valid for specified expiration time
- Anyone with URL can download the object during expiration window
- Expiration is in seconds

```lua
local url, err = storage:presigned_get_url("data/file.txt", { expiration = 7200 })
if err then error(err) end
print("Download URL:", url)
```

#### storage:presigned_put_url(key: string, options?: table) → string, error

Generates a presigned URL for uploading an object without credentials.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| key | string | yes | - | Object key |
| options | table | no | nil | URL options |

**options fields:**

| Field | Type | Default | Notes |
|-------|------|---------|-------|
| expiration | integer | 3600 | Seconds until URL expires (default 1 hour) |
| content_type | string | "" | Expected content type |
| content_length | integer | 0 | Expected content length in bytes |

**Returns:**
- Success: `string` - presigned URL
- Error: `nil, error` - error is structured

**Errors (structured):**

| Condition | Kind |
|-----------|------|
| key empty | errors.INVALID |
| storage released | errors.INVALID |
| operation failed | errors.INTERNAL |

**Yields:** until URL generation completes

**Notes:**
- URL is valid for specified expiration time
- Anyone with URL can upload to the object during expiration window
- content_type and content_length constraints may be enforced by provider

```lua
local url, err = storage:presigned_put_url("data/upload.txt", {
    expiration = 3600,
    content_type = "text/plain",
    content_length = 1024
})
if err then error(err) end
print("Upload URL:", url)
```

#### storage:release() → boolean

Releases the storage resource.

**Returns:** `true` (always)

**Notes:**
- Idempotent - safe to call multiple times
- All subsequent operations will fail with "storage has been released" error
- Resource is automatically released when script completes

```lua
storage:release()
```

## Dependencies

### io.Writer and io.Reader

Used by `download_object` (writer) and `upload_object` (reader/content).

The fs.File type implements both io.Writer and io.Reader:
- io.Writer: file opened with "w" or "a" mode
- io.Reader: file opened with "r" mode

See fs module spec for File type details.

## Errors

This module returns structured errors. Check kind with `errors.*` constants:

```lua
local storage, err = cloudstorage.get("app:storage")
if err then
    if err:kind() == errors.NOT_FOUND then
        -- resource doesn't exist
    elseif err:kind() == errors.INVALID then
        -- bad input or storage released
    end
    error(err:message())
end
```

**Possible kinds:** `errors.INVALID`, `errors.NOT_FOUND`, `errors.INTERNAL`

## Example

```lua
local cloudstorage = require("cloudstorage")
local fs = require("fs")

-- Get storage connection
local storage, err = cloudstorage.get("app.production:s3-bucket")
if err then error(err) end

-- Upload a file
local ok, err = storage:upload_object("backups/data.txt", "Backup data content")
if err then error(err) end

-- List objects
local result, err = storage:list_objects({ prefix = "backups/", max_keys = 10 })
if err then error(err) end

for _, obj in ipairs(result.objects) do
    print(string.format("%s - %d bytes", obj.key, obj.size))
end

-- Download to file
local vol, _ = fs.get("app:temp")
local file, _ = vol:open("/downloaded.txt", "w")
local ok, err = storage:download_object("backups/data.txt", file)
file:close()
if err then error(err) end

-- Generate presigned URL
local url, err = storage:presigned_get_url("backups/data.txt", { expiration = 3600 })
if err then error(err) end
print("Share this URL:", url)

-- Cleanup
storage:delete_objects({"backups/data.txt"})
storage:release()
```
