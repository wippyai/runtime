<!-- SPDX-License-Identifier: MPL-2.0 -->

# hub

Hub module registry browsing and metadata access.

## Summary

`hub` provides read-only access to the Wippy Hub module catalog, versions, and metadata.
All requests support per-call options for registry, token, and timeout.

## API

### `hub.modules.list(opts?)`
List modules with filters.

Options:
- `organization_id` or `org`: string
- `visibility`: `public`, `private`, `internal`
- `type`: `library`, `application`, `agent`, `plugin`
- `sort_order`: `name_asc`, `name_desc`, `created_desc`, `updated_desc`, `downloads_desc`
- `page`: number
- `page_size`: number
- `registry`: string
- `token`: string
- `timeout`: duration (e.g. `"3m"`) or seconds (number)

Returns `{ items, total, page, page_size }`.

### `hub.modules.search(query, opts?)`
Search modules by query and filters.

Options:
- `keywords`: array of strings
- `license`: string
- `include_deprecated`: boolean
- `page`, `page_size`, `registry`, `token`, `timeout`

### `hub.modules.get(module, opts?)`
Fetch a module by `org/name` or module id.

### `hub.modules.readme(module, opts?)`
Fetch the readme for a module.

Options:
- `version`: string or `{ id, version, label }`

Returns `{ content, filename, version }`.

### `hub.versions.list(module, opts?)`
List versions for a module.

Options:
- `include_yanked`: boolean
- `page`, `page_size`, `registry`, `token`, `timeout`

### `hub.versions.get(module, version, opts?)`
Fetch a specific version by `version` or `{ id, version, label }`.

### `hub.versions.inspect(module, version, opts?)`
Download and inspect a version artifact by `version` or `{ id, version, label }`.
Artifacts are cached under `.wippy/vendor` and verified before reuse.
Returns artifact-derived metadata including `{ version, digest, size_bytes, entry_count, entry_kinds, requirements, cache_path }`.

### `hub.dependencies.get(module, version?, opts?)`
Get dependencies for a module version.

### `hub.dependents.get(module, opts?)`
List dependents for a module.

### `hub.files.list(module, version, opts?)`
List files for a module version.

## Examples

```lua
local res, err = hub.modules.search("terminal", {
  keywords = { "cli", "terminal" },
  type = "library",
  sort_order = "downloads_desc",
  page = 1,
  page_size = 20,
  timeout = "3m",
})

if err then
  error(err)
end

print(res.items[1].full_name)
```
