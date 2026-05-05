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
| head_object | (key: string) | table, error | Fetches full metadata for a single object |
| download_object | (key: string, writer: io.Writer, options?: table) | boolean, error | Downloads object to writer |
| upload_object | (key: string, content: string \| io.Reader, options?: table) | boolean, error | Uploads object from string or reader |
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
| include_owner | boolean | false | When true, populate `owner` on each result (S3: FetchOwner=true) |
| include_versions | boolean | false | When true, list every version (S3: ListObjectVersions). `next_continuation_token` then carries S3's `NextKeyMarker` instead of an opaque V2 token, so do not switch `include_versions` mid-pagination. Versioning must be enabled on the bucket; otherwise the response only contains the current "null" version per key. |

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
| content_type | string | MIME type (empty for ListObjectsV2 — use `head_object` to retrieve) |
| etag | string | Entity tag |
| storage_class | string | Storage class (e.g. STANDARD, STANDARD_IA, GLACIER) |
| last_modified | integer | Last-modified timestamp in Unix seconds (omitted if zero) |
| version_id | string | Object version ID (only present when `include_versions = true`) |
| owner | table | `{ id = string, display_name = string }` — only present when `include_owner = true` |

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

#### storage:head_object(key: string) → table, error

Fetches full metadata for a single object, including user-defined metadata (`x-amz-meta-*`).
Useful when you need richer information than `list_objects` provides — list responses do not
include user metadata, and `content_type` is only populated by `head_object` (or after the
provider has set it on upload).

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| key | string | yes | - | Object key |

**Returns:**
- Success: `table` — see fields below
- Error: `nil, error` — structured error

**Result table:**

| Field | Type | Notes |
|-------|------|-------|
| size | integer | Object size in bytes |
| etag | string | Entity tag (RFC 7232 form, including the surrounding `"` quotes) — pass it back as-is to `if_match` / `if_none_match` |
| content_type | string | MIME type |
| cache_control | string | Cache-Control header |
| content_disposition | string | Content-Disposition header |
| content_encoding | string | Content-Encoding header |
| storage_class | string | Storage class |
| version_id | string | Version ID (omitted when empty) |
| last_modified | integer | Last-modified timestamp in Unix seconds (sub-second precision is dropped; omitted if zero) |
| metadata | table<string,string> | User-defined metadata. AWS lowercases keys. Always present — empty table when there is no user metadata. |
| headers | table<string,string> | Raw HTTP response headers (lowercased keys; multi-valued joined with `, `). Escape hatch for provider-specific fields not modeled above (e.g. `x-amz-tagging-count`, `x-amz-replication-status`, `x-amz-server-side-encryption`). Always present — empty when the provider sends no headers. |

**Errors (structured):**

| Condition | Kind |
|-----------|------|
| key empty | errors.INVALID |
| storage released | errors.INVALID |
| object not found | errors.NOT_FOUND |
| operation failed | errors.INTERNAL |

```lua
local head, err = storage:head_object("uploads/photo.jpg")
if err then error(err) end
print(head.content_type, head.size, head.metadata.uploaded_by)
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
| if_match | string | "" | Only download if the object's current ETag matches |
| if_none_match | string | "" | Only download if the object's current ETag does NOT match |

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
| if_match / if_none_match precondition fails | errors.CONFLICT (message `precondition_failed`) |
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

#### storage:upload_object(key: string, content: string | io.Reader, options?: table) → boolean, error

Uploads an object from string or io.Reader. The optional fourth argument carries
metadata and HTTP headers that are sent to the provider, plus optional ETag-based
preconditions.

| Param | Type | Required | Default | Notes |
|-------|------|----------|---------|-------|
| key | string | yes | - | Object key/path |
| content | string \| io.Reader | yes | - | Content as string or reader (e.g., fs.File) |
| options | table | no | nil | Metadata, headers, and preconditions |

**options fields:**

| Field | Type | Default | Notes |
|-------|------|---------|-------|
| content_type | string | "" | Content-Type header |
| cache_control | string | "" | Cache-Control header |
| content_disposition | string | "" | Content-Disposition header |
| content_encoding | string | "" | Content-Encoding header |
| metadata | table<string,string> | nil | User metadata; AWS lowercases keys |
| if_match | string | "" | Only upload if the existing object's ETag matches |
| if_none_match | string | "" | Only upload if no object exists ("*") or its ETag does not match |
| only_if_absent | boolean | false | Friendly alias for `if_none_match = "*"`. When `true`, overrides any explicit `if_none_match`. |
| headers | table<string,string> | nil | Raw HTTP request headers passed verbatim to the provider. Escape hatch for provider-specific options (e.g. `x-amz-tagging`, `x-amz-server-side-encryption`, `x-amz-website-redirect-location`). Headers participate in request signing. |

**Returns:**
- Success: `true`
- Error: `nil, error` - error is structured

**Errors (structured):**

| Condition | Kind |
|-----------|------|
| key empty | errors.INVALID |
| content nil | errors.INVALID |
| storage released | errors.INVALID |
| if_match / if_none_match precondition fails | errors.CONFLICT (message `precondition_failed`) |
| operation failed | errors.INTERNAL |

**Yields:** until upload completes

**Notes:**
- String content is converted to bytes automatically
- io.Reader content (e.g., fs.File) should be opened in read mode ("r")
- File is read completely during upload

```lua
-- Upload string
storage:upload_object("data/hello.txt", "Hello, World!")

-- Upload with metadata and Content-Type
storage:upload_object("data/photo.jpg", bytes, {
    content_type = "image/jpeg",
    cache_control = "max-age=86400",
    metadata = { uploaded_by = "tests", env = "staging" },
})

-- Optimistic concurrency: only upload if no object exists yet.
-- The two forms are equivalent; only_if_absent is the Lua-friendly alias.
local _, err = storage:upload_object("data/once.txt", "first", { if_none_match = "*" })
local _, err = storage:upload_object("data/once.txt", "first", { only_if_absent = true })
if err and err:kind() == errors.CONFLICT then
    -- another writer beat us to it
end
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

**Possible kinds:** `errors.INVALID`, `errors.NOT_FOUND`, `errors.CONFLICT`, `errors.INTERNAL`

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

## Portability notes

The S3 protocol is implemented by many backends (AWS, MinIO, Cloudflare R2,
Backblaze B2, DigitalOcean Spaces, Wasabi, Scaleway, OVH, IBM, Oracle,
Alibaba OSS, Tencent COS, Ceph RGW, etc.) with subtle behavior differences.
The most important caveats:

- **`etag` is not a content checksum.** AWS multipart uploads return
  `<md5>-<partCount>`. SSE-KMS / SSE-C objects return opaque values.
  Backblaze B2 returns SHA-1 shaped like an MD5 string. Use `etag` only for
  round-tripping into `if_match` / `if_none_match` against the same backend.
- **Conditional ops are not universally honored.** AWS, MinIO and R2 respect
  `if_match` / `if_none_match` correctly; some older or self-hosted
  S3-compatible implementations silently ignore them, so `only_if_absent`
  may degrade into "always overwrite." Validate against your backend if
  optimistic concurrency is critical.
- **`version_id`.** Cloudflare R2 (as of writing) and several other
  S3-compatible backends do not implement versioning at all;
  `include_versions = true` returns an empty result or an error there.
  The literal string `"null"` is a valid version_id for objects created
  before versioning was enabled. Don't parse version_id values.
- **`storage_class` values are provider-specific.** Most single-tier
  backends (R2, B2, DO Spaces, Wasabi, MinIO) report `STANDARD` always.
  AWS, Scaleway, IBM, Alibaba and Tencent expose richer tiers with
  different vocabularies. Ceph operators can configure arbitrary
  placement-target names. Code that branches on `storage_class` is
  implicitly coupled to a specific deployment.
- **`metadata`.** Limits vary (AWS ~2 KB total, R2 8 KB, MinIO ~32 KB).
  Stick to ASCII values and stay under 2 KB to be portable. Keys come back
  lowercased on every reasonable provider.
- **`headers` escape-hatch.** Use this when you need a provider-specific
  feature (tagging, SSE, replication) that is not modeled as a typed
  field. The trade-off is that you are now coupled to that backend's
  header conventions.

## Changelog

### [#264](https://github.com/wippyai/runtime/pull/264) — object metadata, ETag, conditional ops, headers escape-hatch

All additions are purely additive; pre-existing call signatures keep working unchanged.

**New method**
- `storage:head_object(key)` → `result, error` — full per-object metadata, the only way to read user metadata (`x-amz-meta-*`).

**`storage:list_objects(options)` new options**
- `include_owner: boolean` — populates `owner` on each result (S3: `FetchOwner=true`).
- `include_versions: boolean` — switches to `ListObjectVersions`, fills `version_id`.

**`storage:list_objects` result — new fields per object**
- `last_modified` (Unix seconds)
- `storage_class` (string, provider-specific)
- `version_id` (only with `include_versions`)
- `owner` (table `{ id, display_name }`, only with `include_owner`)

**`storage:upload_object(key, content, options)` — new optional 4th arg**
- `content_type`, `cache_control`, `content_disposition`, `content_encoding`
- `metadata: table<string,string>` — user metadata (`x-amz-meta-*`)
- `if_match: string`, `if_none_match: string` — conditional upload
- `only_if_absent: boolean` — Lua-friendly alias for `if_none_match = "*"`
- `headers: table<string,string>` — raw HTTP request headers escape-hatch

**`storage:download_object(key, writer, options)` — new option fields**
- `if_match: string`, `if_none_match: string`

**`storage:head_object` result fields**
- `size`, `etag`, `content_type`, `cache_control`, `content_disposition`, `content_encoding`, `storage_class`, `version_id`, `last_modified`
- `metadata: table<string,string>` (always present)
- `headers: table<string,string>` (always present; lowercased keys)

**New error kinds surfaced**
- `errors.NOT_FOUND` — `head_object` / `download_object` on a missing key.
- `errors.CONFLICT` (message `"precondition_failed"`) — when `if_match` / `if_none_match` / `only_if_absent` fails.
