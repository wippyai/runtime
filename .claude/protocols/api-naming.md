# API Naming Convention

Go-idiomatic approach: package and type provide context. Names should be clear at the call site.

## Guiding Principle

Ask: "Is it clear when I read `package.Constant`?"

```go
security.ValidateToken  // clear: validate a token
store.Get               // clear: get from store
stream.Read             // clear: read from stream
cluster.NodeJoined      // clear: node joined event
```

---

## Command ID Constants

**Pattern: `<Action>` or `<Action><Object>`**

Use bare action when package provides context. Add object noun when needed for clarity.

| Package | Pattern | Examples |
|---------|---------|----------|
| function | `<Action>` | `Call`, `AsyncStart`, `AsyncCancel` |
| store | `<Action>` | `Get`, `Set`, `Delete`, `Has` |
| stream | `<Action>` | `Read`, `Write`, `Close`, `Seek`, `Flush`, `Stat` |
| stream (scanner) | `Scanner<Action>` | `ScannerCreate`, `ScannerScan` |
| sql | `<Action>` | `Query`, `Execute`, `Prepare`, `Begin` |
| sql (tx) | `Tx<Action>` | `TxQuery`, `TxExecute`, `TxCommit`, `TxRollback` |
| sql (stmt) | `Stmt<Action>` | `StmtQuery`, `StmtExecute`, `StmtClose` |
| clock | `<Action>` or `<Domain><Action>` | `Sleep`, `TickerStart`, `TickerStop`, `TimerStart` |
| security | `<Action><Object>` | `ValidateToken`, `CreateToken`, `RevokeToken` |
| cloudstorage | `<Action><Object>` | `ListObjects`, `DownloadObject`, `UploadObject` |
| websocket | `<Action>` | `Connect`, `Send`, `Receive`, `Close`, `Ping`, `Subscribe` |
| http | `<Object>` | `Request`, `RequestBatch` |
| event | `<Action>` | `Subscribe`, `Send` |
| contract | `<Action>` | `Open`, `Call`, `AsyncCall`, `AsyncCancel` |
| exec | `<Domain><Action>` | `ProcessWait` |

### Rules

1. No `Cmd` prefix on constants (type `dispatcher.CommandID` provides context)
2. No package name in constant (`store.StoreGet` is redundant)
3. Action comes first in `<Action><Object>` (`ValidateToken` not `TokenValidate`)
4. Use domain prefix when package has multiple domains (`TxQuery`, `StmtQuery`, `TimerStart`)

---

## Command Struct Types

**Pattern: `<Action>Cmd` or `<Domain><Action>Cmd`**

```go
type CallCmd struct { ... }
type ValidateTokenCmd struct { ... }
type TxQueryCmd struct { ... }
```

---

## Response Struct Types

**Pattern: `<Action>Response` or `<Action>Result`**

Use `Result` for simple returns (value + error), `Response` for richer responses.

```go
type QueryResponse struct { ... }
type CallResult struct { ... }
type ValidateTokenResponse struct { ... }
```

---

## Event System Constants

**Pattern: Bare `System`**

```go
const System event.System = "registry"
const System event.System = "cluster"
const System event.System = "process"
```

If package has subsystems, use `System<Name>`:
```go
const System event.System = "temporal"
const SystemTaskQueue event.System = "temporal.taskqueue"
```

---

## Event Kind Constants

**Pattern: `<Domain><Action>` or `<Domain><State>`**

```go
// Entry lifecycle events
EntryCreate  event.Kind = "entry.create"
EntryUpdate  event.Kind = "entry.update"
EntryDelete  event.Kind = "entry.delete"

// State change events (past tense)
NodeJoined   event.Kind = "node.joined"
NodeLeft     event.Kind = "node.left"
LeaderElected event.Kind = "leader.elected"

// Registration events
FactoryRegister event.Kind = "factory.register"
HostRegister    event.Kind = "host.register"
```

### Rules

1. No `EventKind` suffix (type provides context)
2. No `Kind` prefix
3. Domain comes first (`NodeJoined` not `JoinedNode`)
4. Past tense for state changes (`Joined`, `Elected`), present for actions (`Register`, `Delete`)

---

## Error Kind Constants

**Pattern: Bare `<Category>`**

```go
const (
    Unknown          Kind = "Unknown"
    NotFound         Kind = "NotFound"
    AlreadyExists    Kind = "AlreadyExists"
    Invalid          Kind = "Invalid"
    PermissionDenied Kind = "PermissionDenied"
    Timeout          Kind = "Timeout"
)
```

No `Kind` prefix (type `error.Kind` provides context).

---

## Registry Kind Constants

**Pattern: Bare `<Type>` or `<Domain><Type>`**

```go
const (
    Entry               Kind = "registry.entry"
    NamespaceRequirement Kind = "ns.requirement"
    NamespaceDependency  Kind = "ns.dependency"
)
```

No `Kind` prefix.

---

## Error Sentinel Variables

**Pattern: `Err<Description>`** (Go convention)

```go
var (
    ErrNotFound    = errors.New("not found")
    ErrQueueFull   = errors.New("queue full")
    ErrTimeout     = errors.New("timeout")
)
```

---

## Interface Names

**Pattern: Bare `<Role>`** - package provides context, no prefix needed.

```go
queue.Manager      // correct - package provides context
queue.QueueManager // wrong - redundant prefix

store.Store        // correct
env.Service        // correct
env.EnvService     // wrong - redundant
```

Same principle as constants: the package name is part of the identifier at the call site.

---

## Summary Table

| Type | Prefix | Suffix | Example |
|------|--------|--------|---------|
| `dispatcher.CommandID` | None | None | `Call`, `ValidateToken`, `TxQuery` |
| Command struct | None | `Cmd` | `CallCmd`, `ValidateTokenCmd` |
| Response struct | None | `Response`/`Result` | `QueryResponse`, `CallResult` |
| `event.System` | None | None | `System` |
| `event.Kind` | None | None | `NodeJoined`, `EntryCreate` |
| `error.Kind` | None | None | `NotFound`, `Invalid` |
| `registry.Kind` | None | None | `Entry`, `NamespaceRequirement` |
| Error sentinel | `Err` | None | `ErrNotFound`, `ErrTimeout` |
| Interface | None | None | `Manager`, `Store`, `Service` |

---

# Complete Migration List

## Command ID Constants

### api/security/command.go
| Current | Target |
|---------|--------|
| `CmdTokenValidate` | `ValidateToken` |
| `CmdTokenCreate` | `CreateToken` |
| `CmdTokenRevoke` | `RevokeToken` |

### api/store/command.go
| Current | Target |
|---------|--------|
| `CmdStoreGet` | `Get` |
| `CmdStoreSet` | `Set` |
| `CmdStoreDelete` | `Delete` |
| `CmdStoreHas` | `Has` |

### api/service/sql/command.go
| Current | Target |
|---------|--------|
| `CmdQuery` | `Query` |
| `CmdExecute` | `Execute` |
| `CmdPrepare` | `Prepare` |
| `CmdBegin` | `Begin` |
| `CmdStmtQuery` | `StmtQuery` |
| `CmdStmtExecute` | `StmtExecute` |
| `CmdStmtClose` | `StmtClose` |
| `CmdTxQuery` | `TxQuery` |
| `CmdTxExecute` | `TxExecute` |
| `CmdTxPrepare` | `TxPrepare` |
| `CmdTxCommit` | `TxCommit` |
| `CmdTxRollback` | `TxRollback` |

### api/cloudstorage/command.go
| Current | Target |
|---------|--------|
| `CmdListObjects` | `ListObjects` |
| `CmdDownloadObject` | `DownloadObject` |
| `CmdUploadObject` | `UploadObject` |
| `CmdDeleteObjects` | `DeleteObjects` |
| `CmdPresignedGetURL` | `PresignedGetURL` |
| `CmdPresignedPutURL` | `PresignedPutURL` |

### api/websocket/command.go → api/service/websocket/command.go
(File moves to service, see api-structure.md)

| Current | Target |
|---------|--------|
| `CmdWsConnect` | `Connect` |
| `CmdWsSend` | `Send` |
| `CmdWsReceive` | `Receive` |
| `CmdWsClose` | `Close` |
| `CmdWsPing` | `Ping` |
| `CmdWsSubscribe` | `Subscribe` |

### api/service/http/command.go
| Current | Target |
|---------|--------|
| `CmdRequest` | `Request` |
| `CmdRequestBatch` | `RequestBatch` |

### api/stream/command.go
| Current | Target |
|---------|--------|
| `CmdRead` | `Read` |
| `CmdClose` | `Close` |
| `CmdWrite` | `Write` |
| `CmdSeek` | `Seek` |
| `CmdFlush` | `Flush` |
| `CmdStat` | `Stat` |
| `CmdScannerCreate` | `ScannerCreate` |
| `CmdScannerScan` | `ScannerScan` |

### api/clock/command.go
| Current | Target |
|---------|--------|
| `CmdSleep` | `Sleep` |
| `CmdTickerStart` | `TickerStart` |
| `CmdTickerStop` | `TickerStop` |
| `CmdTimerStart` | `TimerStart` |
| `CmdTimerWait` | `TimerWait` |
| `CmdTimerStop` | `TimerStop` |
| `CmdTimerReset` | `TimerReset` |

### api/event/command.go
| Current | Target |
|---------|--------|
| `CmdEventsSubscribe` | `Subscribe` |
| `CmdEventsSend` | `Send` |

### api/contract/command.go
| Current | Target |
|---------|--------|
| `CmdOpen` | `Open` |
| `CmdCall` | `Call` |
| `CmdAsyncCall` | `AsyncCall` |
| `CmdAsyncCancel` | `AsyncCancel` |

### api/service/exec/command.go
| Current | Target |
|---------|--------|
| `CmdProcessWait` | `ProcessWait` |

---

## Command Struct Types

### api/security/command.go
| Current | Target |
|---------|--------|
| `TokenValidateCmd` | `ValidateTokenCmd` |
| `TokenCreateCmd` | `CreateTokenCmd` |
| `TokenRevokeCmd` | `RevokeTokenCmd` |

### api/event/command.go
| Current | Target |
|---------|--------|
| `EventsSubscribeCmd` | `SubscribeCmd` |
| `EventsSendCmd` | `SendCmd` |

### api/websocket/command.go → api/service/websocket/command.go
| Current | Target |
|---------|--------|
| `WsConnectCmd` | `ConnectCmd` |
| `WsSendCmd` | `SendCmd` |
| `WsReceiveCmd` | `ReceiveCmd` |
| `WsCloseCmd` | `CloseCmd` |
| `WsPingCmd` | `PingCmd` |
| `WsSubscribeCmd` | `SubscribeCmd` |
| `WsMessage` | `Message` |
| `WsSubscription` | `Subscription` |

---

## Response Struct Types

### api/security/command.go
| Current | Target |
|---------|--------|
| `TokenValidateResponse` | `ValidateTokenResponse` |
| `TokenCreateResponse` | `CreateTokenResponse` |
| `TokenRevokeResponse` | `RevokeTokenResponse` |

---

## Event Kind Constants

### api/cluster/events.go
| Current | Target |
|---------|--------|
| `NodeJoinedEventKind` | `NodeJoined` |
| `NodeLeftEventKind` | `NodeLeft` |
| `NodeUpdatedEventKind` | `NodeUpdated` |
| `KVPutEventKind` | `KVPut` |
| `KVDeleteEventKind` | `KVDelete` |
| `RaftLeaderElectedEventKind` | `LeaderElected` |
| `RaftLeaderLostEventKind` | `LeaderLost` |

### api/queue/queue.go
| Current | Target |
|---------|--------|
| `KindDriverRegister` | `DriverRegister` |
| `KindDriverDelete` | `DriverDelete` |
| `KindQueueDeclare` | `QueueDeclare` |
| `KindQueueDelete` | `QueueDelete` |

### api/fs/fs.go
| Current | Target |
|---------|--------|
| `KindRegister` | `Register` |
| `KindDelete` | `Delete` |
| `KindAccept` | `Accept` |
| `KindReject` | `Reject` |

### api/registry/registry.go (add domain prefix for clarity)
| Current | Target |
|---------|--------|
| `Create` | `EntryCreate` |
| `Update` | `EntryUpdate` |
| `Delete` | `EntryDelete` |
| `Accept` | `EntryAccept` |
| `Reject` | `EntryReject` |
| `Begin` | `TxBegin` |
| `Commit` | `TxCommit` |
| `Discard` | `TxDiscard` |

---

## Error Kind Constants

### api/error/error.go
| Current | Target |
|---------|--------|
| `KindUnknown` | `Unknown` |
| `KindNotFound` | `NotFound` |
| `KindAlreadyExists` | `AlreadyExists` |
| `KindInvalid` | `Invalid` |
| `KindPermissionDenied` | `PermissionDenied` |
| `KindUnavailable` | `Unavailable` |
| `KindInternal` | `Internal` |
| `KindCanceled` | `Canceled` |
| `KindConflict` | `Conflict` |
| `KindTimeout` | `Timeout` |
| `KindRateLimited` | `RateLimited` |

### api/security/errors.go
| Current | Target |
|---------|--------|
| `KindNotFound` | `NotFound` |
| `KindInvalid` | `Invalid` |
| `KindExpired` | `Expired` |
| `KindRevoked` | `Revoked` |
| `KindDenied` | `Denied` |

### api/supervisor/errors.go
| Current | Target |
|---------|--------|
| `KindTerminated` | `Terminated` |
| `KindExited` | `Exited` |

---

## Registry Kind Constants

### api/registry/registry.go
| Current | Target |
|---------|--------|
| `KindEntry` | `Entry` |
| `KindNamespaceRequirement` | `NamespaceRequirement` |
| `KindNamespaceDependency` | `NamespaceDependency` |

### api/dispatcher/dispatcher.go
| Current | Target |
|---------|--------|
| `KindHandler` | `Handler` |

---

## Already Correct (No Changes Needed)

### api/function/command.go
- `Call`, `AsyncStart`, `AsyncCancel` - correct
- `CallCmd`, `AsyncStartCmd`, `AsyncCancelCmd` - correct
- `CallResult`, `AsyncStartResult` - correct

### api/relay/relay.go
- `HostRegister`, `HostDelete`, `HostAccept`, `HostReject` - correct
- `PeerRegister`, `PeerDelete`, `PeerAccept`, `PeerReject` - correct

### api/process/process.go
- `FactoryRegister`, `FactoryDelete`, `FactoryAccept`, `FactoryReject` - correct

### api/service/temporal/events.go
- `TaskQueueRegister`, `WorkflowRegister` - correct

### api/logs/logs.go
- `Entry`, `SetConfig`, `GetConfig`, `ConfigState` - correct

---

## Error Struct Field Inconsistency

### api/runtime/lua/errors.go
Uses PascalCase fields (inconsistent with rest of codebase):

| Current | Target |
|---------|--------|
| `Msg` | `message` |
| `ErrKind` | `kind` |
| `IsRetryable` | `retryable` |
| `ErrDetails` | `details` |
| `ErrCause` | `cause` |
