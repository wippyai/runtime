# Override Stage

The Override stage applies runtime configuration overrides to registry entries using boot config sections.

## API

```go
import "github.com/ponyruntime/pony/boot/build/stages"

stage := stages.Override()
```

## Configuration Format

Override keys use the format: `namespace:entry:path`

Uses two colons to properly handle dots in namespace and entry names.

### YAML Config Example

```yaml
version: "1.0"
override:
  app:gateway:addr: ":9090"
  app:gateway:tls: true
  app:worker:count: 4
  db:main:connection.host: "db.example.com"
  db:main:connection.port: 5432
  app:cache:meta.priority: "high"
  app.v2:gateway.v1:addr: ":8080"
```

## Path Syntax

- **Simple path**: `app:gateway:addr` → sets `data.addr` on `app:gateway`
- **Explicit data**: `app:gateway:data.addr` → sets `data.addr` on `app:gateway`
- **Nested path**: `db:main:connection.host` → sets `data.connection.host` on `db:main`
- **Meta path**: `app:worker:meta.priority` → sets `meta.priority` on `app:worker`
- **Dots in names**: `app.v2:gateway.v1:addr` → sets `data.addr` on `app.v2:gateway.v1`

## Implementation Details

- Uses `internal/entry.Mutator` for path-based modifications
- Reads from boot config section (configurable section name)
- Errors if target entry not found
- Supports all mutator path features (bare paths, data/meta prefixes, nested paths)
- Two-colon format matches existing runtime config CLI syntax

## Pipeline Usage

```go
pipeline := build.New(
    stages.Link(),
    stages.Override("override"),
)
```
