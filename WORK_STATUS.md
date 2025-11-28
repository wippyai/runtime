# Work Status - Interface Simplification (COMPLETED)

## Completed Tasks

1. **Fixed data race in ReleaseProcessor** (`system/scheduler/scheduler.go:379-382`)
   - Pooled processors are NOT returned to processor pool to avoid race with completeProcessor

2. **Receiver interface** already existed in (`api/relay/pubsub.go:91-94`)
   ```go
   type Receiver interface {
       Send(*Package) error
   }
   ```

3. **Updated `scheduler.Process`** (`system/scheduler/command.go:68-82`)
   - Now embeds `relay.Receiver` instead of separate Send method

4. **Renamed `Poolable` to `Callable`** (`system/funcpool/pool.go:20-30`)
   ```go
   type Callable interface {
       scheduler.Process
       CallAsync(ctx context.Context, input payload.Payloads) error
       Close()
   }
   ```

5. **Renamed `SetupCall` to `CallAsync`** (`runtime/lua/engine2/process.go:474-512`)

6. **Made `ClearExecution` private and auto-called** (`runtime/lua/engine2/process.go`)
   - Renamed to `clearExecution` (line 514-517)
   - Added `p.clearExecution()` before each return of StepDone in Step():
     - Line 194 (layer error)
     - Line 200 (layer error in loop)
     - Line 208 (vmStep error)
     - Line 215 (completion)
     - Line 261 (deadlock error)

7. **Fixed test file** (`runtime/lua/engine2/pool_test.go`)
   - Renamed `PoolableLuaProcess` to `CallableLuaProcess`
   - Updated factory return type from `funcpool.Poolable` to `funcpool.Callable`

## Build Status: PASS
## Test Status: engine2 pool tests PASS

## Final Interface Design

```go
// relay.Receiver (api/relay/pubsub.go)
type Receiver interface {
    Send(*Package) error
}

// scheduler.Process (system/scheduler/command.go)
type Process interface {
    Start(ctx context.Context, input payload.Payloads) error
    Step(results *YieldResults) (StepResult, error)
    relay.Receiver
}

// funcpool.Callable (system/funcpool/pool.go)
type Callable interface {
    scheduler.Process
    CallAsync(ctx context.Context, input payload.Payloads) error
    Close()
}
```

## Key Design Decisions

1. Resources are cleaned up when Step returns StepDone (not lazily on next call)
2. Pooled processors are not returned to sync.Pool to avoid races
3. Process implements relay.Receiver for external message delivery
