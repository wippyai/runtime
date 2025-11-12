# Disable Stage

The Disable stage removes entries from the registry based on patterns defined in boot config.

## API

```go
import "github.com/ponyruntime/pony/boot/build/stages"

stage := stages.Disable()
```

## Configuration Format

Disable patterns are defined in the "disable" config section with two pattern lists:
- `namespaces`: patterns matching entry namespaces
- `entries`: patterns matching full entry IDs (namespace:name format)

### YAML Config Example

```yaml
version: "1.0"
disable:
  namespaces:
    - "test"
    - "dev.**"
    - "app.v1"
  entries:
    - "app:gateway"
    - "db:cache.*"
    - "*.v1:worker"
```

## Wildcard Pattern Syntax

Uses `internal/wildcard` with dot-separated segments:
- `*` - matches exactly one segment
- `**` - matches zero or more segments
- `(a|b|c)` - matches any of the alternatives

### Namespace Pattern Examples

- `"test"` - exact match for namespace "test"
- `"app.**"` - matches "app", "app.v1", "app.v2.beta", etc.
- `"*.v1"` - matches "app.v1", "db.v1", etc.
- `"(app|db)"` - matches either "app" or "db"

### Entry ID Pattern Examples

- `"app:gateway"` - exact match for entry "gateway" in namespace "app"
- `"app:gateway.*"` - matches "app:gateway.v1", "app:gateway.v2", etc.
- `"*.v1:*"` - matches all entries in namespaces ending with ".v1"
- `"app:*"` - matches all entries in namespace "app"
- `"app.v2:gateway.v1"` - handles dots in both namespace and entry name

## How It Works

1. Reads patterns from boot config "disable" section
2. Compiles wildcard matchers for namespaces and entry IDs
3. Filters entries from the registry, removing matches
4. Modifies `*[]registry.Entry` in place

## Error Handling

- Invalid entry pattern (missing `:`): returns error
- Empty patterns: silent success (no-op)
- No config section: silent success (no-op)
- Pattern matches nothing: silent success (no entries removed)

## Implementation Details

- Uses `internal/wildcard.Wildcard` for pattern matching
- Namespace matching: direct match against `entry.ID.NS`
- Entry ID matching: matches both namespace and name parts separately
- Section name constant: `sectionDisable boot.ConfigSection = "disable"`

## Pipeline Usage

```go
pipeline := build.New(
    stages.Disable(),
    stages.Link(),
    stages.Override(),
)
```

Place Disable stage early in pipeline to remove unwanted entries before processing.
