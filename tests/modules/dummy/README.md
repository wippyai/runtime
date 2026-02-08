# wippy/dummy

Simple dummy module for integration testing of the requirement system.

## Installation

```yaml
- name: dependency.dummy
  kind: ns.dependency
  component: wippy/dummy
  version: ">=0.1.0"
```

## Requirements

| Name | Description | Default |
|------|-------------|---------|
| `router` | Router to register endpoints on | `app:router` |

## Endpoints

### GET /dummy/ping

Returns a simple JSON response for testing.

**Response:**
```json
{
  "message": "pong",
  "module": "wippy/dummy"
}
```

## License

MPL-2.0
