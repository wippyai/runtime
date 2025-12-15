# API Folder Structure

## Architecture Pattern

The API folder follows a **contract vs provider** pattern:

```
api/
├── <package>/           # System contract (interface + commands)
└── service/<package>/   # Provider implementations (configs, errors)
```

**Root packages** define system-level contracts (interfaces, commands, events).
**Service packages** contain provider-specific implementations (configs, errors for specific backends).

### Example: Store

```
api/store/           # System contract
├── store.go         # Store interface
├── command.go       # Dispatcher commands (Get, Set, Delete)
└── errors.go        # Contract-level errors

api/service/store/   # Provider implementations
├── memory/          # Memory provider config
│   ├── config.go
│   └── errors.go
└── sql/             # SQL provider config
    ├── config.go
    └── errors.go
```

This is the **correct pattern** - contract in root, provider configs in service.

---

## Current Structure Issues

### 1. Websocket - Move to Service

**Problem**: `api/websocket/` contains only `command.go` (dispatcher commands), while `api/service/websocket/` contains `errors.go`. These should be together as it's a dispatcher service, not a system contract.

**Action**: Move `api/websocket/command.go` → `api/service/websocket/command.go`

```
# Before
api/websocket/command.go
api/service/websocket/errors.go

# After
api/service/websocket/
├── command.go
└── errors.go
```

Then delete `api/websocket/` directory.

---

### 2. Redundant Queue Nesting

**Problem**: `api/service/queue/queue/` has redundant "queue" in path.

**Action**: Flatten the structure.

```
# Before
api/service/queue/
├── consumer/
│   ├── config.go
│   └── errors.go
├── memory/
│   └── config.go
└── queue/           # redundant nesting
    ├── config.go
    └── errors.go

# After
api/service/queue/
├── consumer/
│   ├── config.go
│   └── errors.go
├── memory/
│   └── config.go
├── config.go        # moved from queue/
└── errors.go        # moved from queue/
```

---

### 3. Supervisor Disambiguation

**Problem**: Two supervisor packages exist with different purposes:
- `api/supervisor/` - Service lifecycle management (full implementation)
- `api/service/supervisor/` - Service-level config

**Action**: Rename `api/supervisor/` to clarify it's the lifecycle manager.

Options:
- `api/supervisor/` → `api/lifecycle/`
- Or keep as-is but document the distinction

The current split is actually valid:
- `api/supervisor/` = lifecycle contract (interfaces for service management)
- `api/service/supervisor/` = supervisor service provider config

---

## Valid Dual Packages (No Changes Needed)

These follow the contract vs provider pattern correctly:

| Root (Contract) | Service (Provider) | Purpose |
|-----------------|-------------------|---------|
| `api/store/` | `api/service/store/` | Store interface vs memory/sql configs |
| `api/queue/` | `api/service/queue/` | Queue interface vs consumer/memory configs |
| `api/supervisor/` | `api/service/supervisor/` | Lifecycle interface vs supervisor config |

---

## Migration Checklist

### High Priority

- [ ] Move `api/websocket/command.go` to `api/service/websocket/`
- [ ] Delete empty `api/websocket/` directory
- [ ] Flatten `api/service/queue/queue/*` to `api/service/queue/`

### Medium Priority

- [ ] Consider renaming `api/supervisor/` to `api/lifecycle/` for clarity

### Low Priority

- [ ] Document the contract vs provider pattern in API README

---

## Final Target Structure

```
api/
├── attrs/              # Attributes (foundation)
├── boot/               # Bootstrap/loading
├── clock/              # Time operations
├── cloudstorage/       # Cloud storage contract
├── cluster/            # Cluster operations
├── context/            # Context management (foundation)
├── contract/           # Contract/binding definitions
├── dispatcher/         # Command dispatch
├── env/                # Environment variables
├── error/              # Error types (foundation)
├── event/              # Event bus (foundation)
├── fs/                 # Filesystem contract
├── function/           # Function execution
├── logs/               # Logging
├── metrics/            # Metrics
├── payload/            # Payload format (foundation)
├── pid/                # Process identifiers (foundation)
├── process/            # Process abstractions
├── queue/              # Queue contract
├── registry/           # Registry contract
├── relay/              # Inter-process messaging
├── resource/           # Resource management
├── runtime/            # Runtime/task execution
│   ├── lua/            # Lua runtime specifics
│   └── resource/       # Resource tables
├── security/           # Security/auth contract
├── store/              # Store contract
├── stream/             # Stream operations
├── supervisor/         # Lifecycle management (or rename to lifecycle/)
├── topology/           # Process topology
└── service/            # Provider implementations
    ├── aws/
    │   ├── config/
    │   └── s3/
    ├── di/
    ├── env/
    ├── exec/
    ├── fs/
    │   ├── directory/
    │   └── embed/
    ├── host/
    ├── http/
    ├── metrics/
    ├── otel/
    ├── queue/
    │   ├── consumer/
    │   └── memory/
    │   # config.go and errors.go at this level (flattened)
    ├── security/
    │   ├── policy/
    │   └── tokenstore/
    ├── sql/
    ├── store/
    │   ├── memory/
    │   └── sql/
    ├── supervisor/
    ├── template/
    ├── temporal/
    ├── terminal/
    └── websocket/      # includes command.go after move
```
