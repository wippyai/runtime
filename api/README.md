# API Architecture

The `api/` tree defines public contracts and configuration for the runtime.
It is intentionally stable and minimal. Implementation details live elsewhere.

## Structure

```
api/<domain>/           Contract: interfaces, commands, events, errors.
api/service/<domain>/   Provider config and provider-specific errors.
```

Examples:
- `api/store/` defines the store contract.
- `api/service/store/` defines memory/sql provider config for stores.

## Boundary Rules

- `api/**` must not import `github.com/wippyai/runtime/service` packages.
- `api/**` should avoid implementation-specific constructors; keep only sentinels.
- Provider-specific validation and errors live under `api/service/**`.

## Naming Conventions

Follow `.claude/protocols/api-naming.md`. Key points:
- Command IDs: action-first, no redundant prefixes.
- Event kinds: include domain when ambiguous (`EntryCreate`, `FunctionAccept`).
- Error kinds: use `api/error.Kind` and `api/error` builders.

## Error Guidance

- Prefer `api/error` for all contract-level errors.
- Include `Retryable` and `Details` for structured handling.
- Keep API errors stable; avoid leaking internal messages.

## Tests

- API tests should validate contract behavior only.
- Do not import service implementations in API tests.
