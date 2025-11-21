# AMQP Queue Driver - Complete Design Document

**Date:** 2025-11-19
**Status:** Design Phase
**Author:** Claude (with extensive research)

---

## Table of Contents

1. [Executive Summary](#executive-summary)
2. [Research Summary](#research-summary)
3. [Architecture Decisions](#architecture-decisions)
4. [File Structure](#file-structure)
5. [Core Implementation Details](#core-implementation-details)
6. [Configuration Design](#configuration-design)
7. [Error Handling](#error-handling)
8. [Testing Strategy](#testing-strategy)
9. [Implementation Phases](#implementation-phases)
10. [Open Questions](#open-questions)

---

## Executive Summary

This document specifies a production-ready AMQP queue driver for wippy's queue system. The design integrates:

- **Wippy's architecture**: Driver interface, supervisor.Service, api/error.Error, attrs.Attributes
- **RoadRunner's patterns**: Buffered channel reuse (cap 1), redial with backoff, notification monitoring
- **Production requirements**: High throughput, automatic reconnection, TLS support, comprehensive testing

**Key Metrics:**
- ~2,620 lines of code across 12 files
- 12-day implementation timeline
- Target: >10k msg/sec publish throughput
- Zero allocations on hot path (after warmup)

---

## Research Summary

### Sources Analyzed

1. **RoadRunner AMQP Plugin** (github.com/roadrunner-server/amqp)
   - File: `amqpjobs/driver.go`, `listener.go`, `redial.go`, `config.go`
   - Key findings: Buffered channels (cap 1), exponential backoff redial, fire-and-forget acks

2. **wagslane/go-rabbitmq** (popular Go wrapper)
   - Key findings: Automatic reconnection is #1 feature, per-operation channels, handler pattern for consumers

3. **RabbitMQ Official Go Tutorial**
   - Key findings: Queue declaration idempotency, defer Close() pattern, simple dial/channel model

4. **Wippy Internal Code**
   - `service/queue/memory/driver.go` - Memory queue pattern (4 files: driver, manager, tests)
   - `api/queue/driver.go` - Driver interface contract
   - `api/error/error.go` - Structured error API
   - `internal/backoff/` - Exponential backoff calculator
   - `internal/entry/` - Config decoding utilities

### Key Insights

**RoadRunner's Buffered Channel Pattern is Correct for Wippy:**
- High-throughput requirement (Lua functions publishing messages)
- Zero allocation after warmup
- AMQP channels are designed for reuse (multiplexing primitives)
- Proven in production with PHP worker pools (similar to Lua workers)

**Buffered Channel (cap 1) vs Per-Operation Channels:**

| Approach | Pros | Cons | Verdict |
|----------|------|------|---------|
| Per-operation (wagslane) | Simple, no sync | Channel creation overhead, doesn't scale | ❌ Wrong for wippy |
| Buffered chan cap=1 (RR) | Zero alloc, scales well, minimal overhead | Slightly complex | ✅ Correct for wippy |

**Reconnection Strategy:**
- Internal reconnection (not supervisor-level restart) is correct
- Preserves in-flight state, faster recovery
- Use wippy's `internal/backoff` for exponential backoff
- Monitor `conn.NotifyClose()` for connection drops

---

## Architecture Decisions

### 1. Connection Management

**Pattern: Single Connection + Buffered Channels**

```go
type Driver struct {
    conn     *amqp.Connection
    connLock sync.RWMutex

    // Buffered channels (cap 1) for channel reuse
    publishChan chan *amqp.Channel  // Reused for all publishes
    consumeChan chan *amqp.Channel  // Reused for consumers
    stateChan   chan *amqp.Channel  // Reused for topology ops

    // Notification channels for redial
    connNotify    chan *amqp.Error
    publishNotify chan *amqp.Error
    consumeNotify chan *amqp.Error
    stateNotify   chan *amqp.Error

    redialCh chan error
}
```

**Rationale:**
- One connection per driver (AMQP best practice)
- Three channels: publish, consume, state (topology)
- Buffered chan (cap 1) = lock-free synchronization
- Notification channels detect connection/channel failures

### 2. Reconnection Strategy

**Pattern: Redial Loop with Exponential Backoff**

```go
func (d *Driver) redialLoop() {
    calc := backoff.NewCalculator(supervisor.RetryPolicy{
        InitialDelay:  d.config.ReconnectDelay,    // 1s default
        MaxDelay:      d.config.MaxReconnectDelay, // 30s default
        BackoffFactor: 2.0,
        Jitter:        0.1,
    })

    for {
        select {
        case <-d.ctx.Done():
            return
        case <-d.connNotify:
            d.reconnectWithBackoff(calc)
        }
    }
}
```

**Why not supervisor-level restart?**
- Supervisor restarts = lose all state (queue mappings, consumers)
- Reconnection = preserve state, faster recovery
- Supervisor handles driver crashes, reconnection handles network issues

### 3. Channel Reuse Pattern

**Publish (hot path):**

```go
func (d *Driver) Publish(ctx, queue, msgs) error {
    // Get channel (blocks if in use)
    ch := <-d.publishChan
    defer func() { d.publishChan <- ch }()

    for _, msg := range msgs {
        ch.PublishWithContext(ctx, "", queueName, false, false, pub)
    }
    return nil
}
```

**Benefits:**
- Zero allocation (channel already exists)
- Minimal AMQP protocol overhead (no channel.open/close)
- Natural synchronization (buffered chan blocks concurrent access)

### 4. Configuration Layers

**Two-level configuration (wippy pattern):**

1. **Driver-level** (AMQP-specific, typed config):
   - Connection: DSN, TLS, Prefetch
   - Reconnection: ReconnectDelay, MaxReconnectDelay
   - Lifecycle: supervisor.LifecycleConfig

2. **Queue-level** (universal, via attrs.Attributes):
   - OptionDurable, OptionExclusive, OptionAutoDelete
   - OptionMaxLength, OptionMessageTTL
   - OptionDeadLetterExchange, OptionMaxRetryCount

**Rationale:**
- Driver config is AMQP-specific (other drivers have different needs)
- Queue options are universal (work across memory, AMQP, Kafka, etc.)
- Follows existing memory driver pattern

### 5. Error Handling

**Pattern: api/error.Error Interface**

All errors implement wippy's structured error API:

```go
type AMQPError struct {
    kind       apierror.Kind       // NotFound, Unavailable, Invalid, etc.
    retryable  apierror.Ternary    // True, False, Unknown
    message    string
    underlying error
    details    attrs.Bag
}
```

**Error Categories:**

| Error | Kind | Retryable | Strategy |
|-------|------|-----------|----------|
| Connection closed | KindUnavailable | True | Exponential backoff |
| Queue not found | KindNotFound | False | Fail fast |
| Invalid config | KindInvalid | False | Fail fast |
| Driver stopping | KindCanceled | False | Graceful exit |

---

## File Structure

### Service Layer (12 files, ~2,620 lines)

```
service/queue/amqp/
├── driver.go              (~250 lines)
│   - Driver struct with buffered channels
│   - Implement queueapi.Driver interface
│   - Start/Stop lifecycle (supervisor.Service)
│   - Queue tracking map
│
├── driver_test.go         (~300 lines)
│   - Unit tests with mocked connections
│   - Test all Driver methods
│   - Lifecycle tests
│
├── manager.go             (~150 lines)
│   - Registry listener (Add/Update/Delete)
│   - Driver creation from config
│   - Uses internal/entry for decoding
│
├── manager_test.go        (~250 lines)
│   - Manager lifecycle tests
│   - Config decoding tests
│
├── connection.go          (~200 lines)
│   - dial() with TLS support
│   - Channel creation (with publisher confirms)
│   - Notification setup
│   - Close() cleanup
│
├── redial.go              (~150 lines)
│   - redialLoop() monitoring notifyClose
│   - reconnectWithBackoff() using internal/backoff
│   - Channel recreation after reconnect
│
├── topology.go            (~180 lines)
│   - DeclareQueue() with attrs mapping
│   - buildQueueArgs() (AMQP Table)
│   - declareDLX() for dead letter exchange
│   - GetQueueInfo() for stats
│
├── publish.go             (~120 lines)
│   - Publish() with buffered channel pattern
│   - Error handling
│
├── consume.go             (~150 lines)
│   - Attach() implementation
│   - consumeLoop() with reconnection
│   - handleDeliveries() forwarding
│
├── convert.go             (~100 lines)
│   - convertToAMQPPublishing()
│   - convertFromAMQPDelivery()
│   - Header mapping (standard + trace context)
│   - Ack/Nack callback creation
│
├── errors.go              (~80 lines)
│   - AMQPError struct (implements api/error.Error)
│   - Error constructors for all categories
│   - ErrConnectionFailed, ErrPublishFailed, etc.
│
└── integration_test.go    (~400 lines)
    - Docker RabbitMQ tests
    - TestIntegration_PublishConsume
    - TestIntegration_Reconnection
    - TestIntegration_DLX
    - TestIntegration_QueueStats
    - TestIntegration_TLS
```

### API Layer (2 files, ~350 lines)

```
api/service/queue/amqp/
├── config.go              (~200 lines)
│   - Config struct
│   - TLSConfig struct
│   - Validate() and InitDefaults()
│   - TLS certificate loading
│
└── config_test.go         (~150 lines)
    - Config validation tests
    - TLS config tests
```

### Boot Layer (2 files, ~40 lines)

```
boot/components/queue/
├── amqp.go                (~40 lines)
│   - Bootstrap component
│   - Registers manager as registry listener
│
└── constants.go           (update)
    - Add AMQPDriverName constant
```

### Testing Infrastructure

```
tests/
└── docker-compose.yml     (update)
    - Add RabbitMQ service
    - Configure healthcheck
```

**File Organization Rationale:**
- Follows wippy pattern (memory driver is 4 files)
- Rational split based on complexity and testability
- connection.go + redial.go = complex state management (separate for tests)
- topology.go = AMQP-specific declarations (isolated concern)
- publish.go + consume.go = message flow (clear separation)
- Not over-split (12 files vs RR's 9 files)

---

## Core Implementation Details

### 1. Driver Struct

```go
type Driver struct {
    id     registry.ID
    config *amqpapi.Config
    logger *zap.Logger

    // Connection
    conn     *amqp.Connection
    connLock sync.RWMutex

    // Buffered channels (cap 1) for channel reuse
    publishChan chan *amqp.Channel
    consumeChan chan *amqp.Channel
    stateChan   chan *amqp.Channel

    // Notification channels for redial
    connNotify    chan *amqp.Error
    publishNotify chan *amqp.Error
    consumeNotify chan *amqp.Error
    stateNotify   chan *amqp.Error

    // Lifecycle
    ctx        context.Context
    cancel     context.CancelFunc
    started    atomic.Bool
    stopped    atomic.Bool
    listeners  atomic.Uint32  // Active consumer count
    wg         sync.WaitGroup
    statusChan chan any

    // Redial
    redialCh chan error

    // Queue tracking
    queues map[registry.ID]string  // queueID -> queueName
    mu     sync.RWMutex
}
```

### 2. Lifecycle Methods

**Start:**

```go
func (d *Driver) Start(ctx context.Context) (<-chan any, error) {
    if !d.started.CompareAndSwap(false, true) {
        return d.statusChan, nil
    }

    d.ctx, d.cancel = context.WithCancel(ctx)
    d.statusChan = make(chan any, 1)

    // Initial connection
    if err := d.dial(); err != nil {
        return nil, ErrConnectionFailed(err)
    }

    // Start redial loop
    go d.redialLoop()

    return d.statusChan, nil
}
```

**Stop:**

```go
func (d *Driver) Stop(ctx context.Context) error {
    if !d.stopped.CompareAndSwap(false, true) {
        return nil
    }

    // Cancel all consumers
    d.cancel()

    // Wait for consumers to finish (with timeout from ctx)
    done := make(chan struct{})
    go func() {
        d.wg.Wait()
        close(done)
    }()

    select {
    case <-done:
        // Clean shutdown
    case <-ctx.Done():
        // Context timeout/cancelled
        d.logger.Warn("shutdown timeout", zap.Error(ctx.Err()))
        return ctx.Err()
    }

    // Close connection
    d.connLock.Lock()
    defer d.connLock.Unlock()

    if d.conn != nil {
        return d.conn.Close()
    }

    return nil
}
```

### 3. Connection Management

**Dial:**

```go
func (d *Driver) dial() error {
    var conn *amqp.Connection
    var err error

    if d.config.TLS != nil && d.config.TLS.Enabled {
        tlsConfig, err := d.config.TLS.Build()
        if err != nil {
            return err
        }
        conn, err = amqp.DialTLS(d.config.DSN, tlsConfig)
    } else {
        conn, err = amqp.Dial(d.config.DSN)
    }

    if err != nil {
        return err
    }

    d.connLock.Lock()
    d.conn = conn
    d.connLock.Unlock()

    // Create channels with publisher confirms
    publishCh, err := conn.Channel()
    if err != nil {
        conn.Close()
        return err
    }
    if err := publishCh.Confirm(false); err != nil {
        conn.Close()
        return err
    }

    consumeCh, err := conn.Channel()
    if err != nil {
        conn.Close()
        return err
    }

    stateCh, err := conn.Channel()
    if err != nil {
        conn.Close()
        return err
    }

    // Set prefetch on all channels
    for _, ch := range []*amqp.Channel{publishCh, consumeCh, stateCh} {
        if err := ch.Qos(d.config.Prefetch, 0, false); err != nil {
            conn.Close()
            return err
        }
    }

    // Put channels in buffered chans
    d.publishChan <- publishCh
    d.consumeChan <- consumeCh
    d.stateChan <- stateCh

    // Setup notifications
    d.connNotify = conn.NotifyClose(make(chan *amqp.Error, 1))
    d.publishNotify = publishCh.NotifyClose(make(chan *amqp.Error, 1))
    d.consumeNotify = consumeCh.NotifyClose(make(chan *amqp.Error, 1))
    d.stateNotify = stateCh.NotifyClose(make(chan *amqp.Error, 1))

    d.logger.Info("AMQP connection established")
    return nil
}
```

**Redial:**

```go
func (d *Driver) redialLoop() {
    calc := backoff.NewCalculator(supervisor.RetryPolicy{
        InitialDelay:  d.config.ReconnectDelay,
        MaxDelay:      d.config.MaxReconnectDelay,
        BackoffFactor: 2.0,
        Jitter:        0.1,
        MaxAttempts:   0, // Infinite retries
    })

    for {
        select {
        case <-d.ctx.Done():
            return
        case err := <-d.connNotify:
            d.logger.Warn("connection closed, reconnecting", zap.Error(err))
            d.reconnectWithBackoff(calc)
        }
    }
}

func (d *Driver) reconnectWithBackoff(calc *backoff.Calculator) {
    // Drain old channels
    select {
    case <-d.publishChan:
    default:
    }
    select {
    case <-d.consumeChan:
    default:
    }
    select {
    case <-d.stateChan:
    default:
    }

    calc.Reset()

    for {
        if d.stopped.Load() {
            return
        }

        interval := calc.NextInterval()
        if interval == 0 {
            d.logger.Error("max reconnection attempts reached")
            d.statusChan <- supervisor.EventFailed{Error: ErrConnectionFailed(nil)}
            return
        }

        d.logger.Info("attempting reconnection", zap.Duration("delay", interval))

        select {
        case <-d.ctx.Done():
            return
        case <-time.After(interval):
        }

        if err := d.dial(); err != nil {
            d.logger.Error("reconnection failed", zap.Error(err))
            continue
        }

        calc.Reset()
        d.logger.Info("reconnection successful")
        return
    }
}
```

### 4. Message Conversion

**wippy.Message → amqp.Publishing:**

```go
func convertToAMQPPublishing(msg *queueapi.Message) amqp.Publishing {
    pub := amqp.Publishing{
        MessageId:   msg.ID,
        Body:        msg.Body.Bytes(),
        Headers:     make(amqp.Table),
        Timestamp:   time.Now(),
        ContentType: "application/octet-stream",
    }

    // Map standard headers
    if ct := msg.Headers.GetString(queueapi.HeaderContentType, ""); ct != "" {
        pub.ContentType = ct
    }

    if priority := msg.Headers.GetInt(queueapi.HeaderPriority, 0); priority > 0 {
        pub.Priority = uint8(priority)
    }

    if corrID := msg.Headers.GetString(queueapi.HeaderCorrelationID, ""); corrID != "" {
        pub.CorrelationId = corrID
    }

    if replyTo := msg.Headers.GetString(queueapi.HeaderReplyTo, ""); replyTo != "" {
        pub.ReplyTo = replyTo
    }

    if ttl := msg.Headers.GetDuration(queueapi.HeaderTTL, 0); ttl > 0 {
        pub.Expiration = fmt.Sprintf("%d", ttl.Milliseconds())
    }

    // Copy all headers to AMQP table
    for key, value := range msg.Headers {
        pub.Headers[key] = value
    }

    return pub
}
```

**amqp.Delivery → wippy.Delivery:**

```go
func convertFromAMQPDelivery(del *amqp.Delivery) *queueapi.Delivery {
    msg := queueapi.NewMessageWithID(del.MessageId, payload.New(del.Body))

    // Map standard headers
    if del.ContentType != "" {
        msg.Headers.Set(queueapi.HeaderContentType, del.ContentType)
    }

    if del.Priority > 0 {
        msg.Headers.Set(queueapi.HeaderPriority, int(del.Priority))
    }

    if del.CorrelationId != "" {
        msg.Headers.Set(queueapi.HeaderCorrelationID, del.CorrelationId)
    }

    if !del.Timestamp.IsZero() {
        msg.Headers.Set(queueapi.HeaderTimestamp, del.Timestamp.Unix())
    }

    // Copy AMQP headers
    for key, value := range del.Headers {
        msg.Headers.Set(key, value)
    }

    // Track delivery count
    if xDeathCount, ok := del.Headers["x-delivery-count"].(int32); ok {
        msg.Headers.Set(queueapi.HeaderDeliveryCount, int(xDeathCount))
    }

    return &queueapi.Delivery{
        Message: msg,
        Ack: func(ctx context.Context) error {
            return del.Ack(false)
        },
        Nack: func(ctx context.Context) error {
            return del.Nack(false, true) // requeue
        },
    }
}
```

### 5. Queue Topology

**DeclareQueue:**

```go
func (d *Driver) DeclareQueue(ctx context.Context, queueID registry.ID, opts attrs.Attributes) error {
    if !d.started.Load() {
        return ErrDriverNotStarted()
    }

    queueName := opts.GetString(queueapi.OptionQueueName, queueID.Name)

    // Get channel from state buffer
    ch := <-d.stateChan
    defer func() { d.stateChan <- ch }()

    // Build queue arguments
    args := make(amqp.Table)

    if maxLen := opts.GetInt(queueapi.OptionMaxLength, 0); maxLen > 0 {
        args["x-max-length"] = int32(maxLen)
    }

    if maxBytes := opts.GetInt(queueapi.OptionMaxBytes, 0); maxBytes > 0 {
        args["x-max-length-bytes"] = int32(maxBytes)
    }

    if ttl := opts.GetDuration(queueapi.OptionMessageTTL, 0); ttl > 0 {
        args["x-message-ttl"] = int32(ttl.Milliseconds())
    }

    if dlx := opts.GetString(queueapi.OptionDeadLetterExchange, ""); dlx != "" {
        args["x-dead-letter-exchange"] = dlx

        // Declare DLX
        if err := ch.ExchangeDeclare(dlx, "fanout", true, false, false, false, nil); err != nil {
            return ErrQueueDeclarationFailed(err, queueName)
        }
    }

    // Declare queue
    _, err := ch.QueueDeclare(
        queueName,
        opts.GetBool(queueapi.OptionDurable, true),
        opts.GetBool(queueapi.OptionAutoDelete, false),
        opts.GetBool(queueapi.OptionExclusive, false),
        false, // No-wait
        args,
    )

    if err != nil {
        return ErrQueueDeclarationFailed(err, queueName)
    }

    // Store queue mapping
    d.mu.Lock()
    d.queues[queueID] = queueName
    d.mu.Unlock()

    d.logger.Info("queue declared",
        zap.String("id", queueID.String()),
        zap.String("queue", queueName))

    return nil
}
```

---

## Configuration Design

### Driver Config (api/service/queue/amqp/config.go)

```go
package amqp

import (
    "crypto/tls"
    "crypto/x509"
    "fmt"
    "os"
    "time"

    "github.com/wippyai/runtime/api/registry"
    "github.com/wippyai/runtime/api/supervisor"
)

const Kind registry.Kind = "queue.driver.amqp"

type Config struct {
    // Connection
    DSN      string `json:"dsn"`      // amqp://user:pass@host:port/vhost
    Prefetch int    `json:"prefetch"` // Global prefetch count

    // Reconnection
    ReconnectDelay    time.Duration `json:"reconnect_delay"`
    MaxReconnectDelay time.Duration `json:"max_reconnect_delay"`

    // TLS
    TLS *TLSConfig `json:"tls,omitempty"`

    // Lifecycle
    Lifecycle supervisor.LifecycleConfig `json:"lifecycle"`
}

type TLSConfig struct {
    Enabled  bool   `json:"enabled"`
    CertFile string `json:"cert_file,omitempty"`
    KeyFile  string `json:"key_file,omitempty"`
    CAFile   string `json:"ca_file,omitempty"`
    Insecure bool   `json:"insecure"` // Skip verification (dev only)
}

func (c *Config) Validate() error {
    if c.DSN == "" {
        return fmt.Errorf("dsn is required")
    }

    if c.Prefetch < 0 {
        return fmt.Errorf("prefetch must be >= 0")
    }

    if c.ReconnectDelay < 0 {
        return fmt.Errorf("reconnect_delay must be >= 0")
    }

    if c.MaxReconnectDelay < c.ReconnectDelay {
        return fmt.Errorf("max_reconnect_delay must be >= reconnect_delay")
    }

    if c.TLS != nil && c.TLS.Enabled {
        if err := c.TLS.Validate(); err != nil {
            return fmt.Errorf("tls config: %w", err)
        }
    }

    return nil
}

func (c *Config) InitDefaults() {
    if c.Prefetch == 0 {
        c.Prefetch = 10
    }
    if c.ReconnectDelay == 0 {
        c.ReconnectDelay = time.Second
    }
    if c.MaxReconnectDelay == 0 {
        c.MaxReconnectDelay = 30 * time.Second
    }
    c.Lifecycle.InitDefaults()
}

func (t *TLSConfig) Validate() error {
    if !t.Enabled {
        return nil
    }

    if t.CertFile != "" && t.KeyFile == "" {
        return fmt.Errorf("key_file required when cert_file is set")
    }

    if t.KeyFile != "" && t.CertFile == "" {
        return fmt.Errorf("cert_file required when key_file is set")
    }

    return nil
}

func (t *TLSConfig) Build() (*tls.Config, error) {
    if !t.Enabled {
        return nil, nil
    }

    config := &tls.Config{
        InsecureSkipVerify: t.Insecure,
    }

    // Load client certificate
    if t.CertFile != "" && t.KeyFile != "" {
        cert, err := tls.LoadX509KeyPair(t.CertFile, t.KeyFile)
        if err != nil {
            return nil, fmt.Errorf("load client cert: %w", err)
        }
        config.Certificates = []tls.Certificate{cert}
    }

    // Load CA certificate
    if t.CAFile != "" {
        caCert, err := os.ReadFile(t.CAFile)
        if err != nil {
            return nil, fmt.Errorf("read CA cert: %w", err)
        }

        caCertPool := x509.NewCertPool()
        if !caCertPool.AppendCertsFromPEM(caCert) {
            return nil, fmt.Errorf("invalid CA certificate")
        }
        config.RootCAs = caCertPool
    }

    return config, nil
}
```

### Example Configuration

```yaml
# Registry entry for AMQP driver
registry:
  queue.driver.amqp:my-rabbitmq:
    kind: queue.driver.amqp
    data:
      dsn: amqp://user:pass@rabbitmq.example.com:5672/myapp
      prefetch: 20
      reconnect_delay: 2s
      max_reconnect_delay: 60s
      tls:
        enabled: true
        cert_file: /etc/certs/client.crt
        key_file: /etc/certs/client.key
        ca_file: /etc/certs/ca.crt
      lifecycle:
        auto_start: true
        start_timeout: 30s
        stop_timeout: 15s

  # Queue declaration using the driver
  queue.queue:orders:
    kind: queue.queue
    driver: queue.driver.amqp:my-rabbitmq
    data:
      options:
        durable: true
        max_length: 10000
        message_ttl: 3600s
        dead_letter_exchange: orders.dlx

  # Consumer for the queue
  queue.consumer:order-processor:
    kind: queue.consumer
    queue: queue.queue:orders
    func: func:process-order
    data:
      concurrency: 5
      prefetch: 10
```

---

## Error Handling

### Error Interface Implementation

```go
// service/queue/amqp/errors.go

package amqp

import (
    "fmt"

    "github.com/wippyai/runtime/api/attrs"
    apierror "github.com/wippyai/runtime/api/error"
)

type AMQPError struct {
    kind       apierror.Kind
    retryable  apierror.Ternary
    message    string
    underlying error
    details    attrs.Bag
}

func (e *AMQPError) Error() string {
    if e.underlying != nil {
        return fmt.Sprintf("%s: %v", e.message, e.underlying)
    }
    return e.message
}

func (e *AMQPError) Kind() apierror.Kind         { return e.kind }
func (e *AMQPError) Retryable() apierror.Ternary { return e.retryable }
func (e *AMQPError) Details() attrs.Attributes   { return e.details }
func (e *AMQPError) Unwrap() error               { return e.underlying }

// Connection errors
func ErrConnectionFailed(err error) *AMQPError {
    return &AMQPError{
        kind:       apierror.KindUnavailable,
        retryable:  apierror.True,
        message:    "AMQP connection failed",
        underlying: err,
        details:    attrs.NewBag(),
    }
}

func ErrConnectionClosed() *AMQPError {
    return &AMQPError{
        kind:      apierror.KindUnavailable,
        retryable: apierror.True,
        message:   "AMQP connection closed",
        details:   attrs.NewBag(),
    }
}

// Publish errors
func ErrPublishFailed(err error, queue string) *AMQPError {
    bag := attrs.NewBag()
    bag.Set("queue", queue)

    return &AMQPError{
        kind:       apierror.KindUnavailable,
        retryable:  apierror.True,
        message:    "message publish failed",
        underlying: err,
        details:    bag,
    }
}

func ErrQueueNotFound(queue string) *AMQPError {
    bag := attrs.NewBag()
    bag.Set("queue", queue)

    return &AMQPError{
        kind:      apierror.KindNotFound,
        retryable: apierror.False,
        message:   "queue not declared",
        details:   bag,
    }
}

// Topology errors
func ErrQueueDeclarationFailed(err error, queue string) *AMQPError {
    bag := attrs.NewBag()
    bag.Set("queue", queue)

    return &AMQPError{
        kind:       apierror.KindInvalid,
        retryable:  apierror.False,
        message:    "queue declaration failed",
        underlying: err,
        details:    bag,
    }
}

// Driver lifecycle errors
func ErrDriverNotStarted() *AMQPError {
    return &AMQPError{
        kind:      apierror.KindUnavailable,
        retryable: apierror.False,
        message:   "driver not started",
        details:   attrs.NewBag(),
    }
}

func ErrDriverStopping() *AMQPError {
    return &AMQPError{
        kind:      apierror.KindCanceled,
        retryable: apierror.False,
        message:   "driver is stopping",
        details:   attrs.NewBag(),
    }
}
```

### Error Handling Strategy

| Error Scenario | Kind | Retryable | Action |
|----------------|------|-----------|--------|
| Connection lost | Unavailable | True | Trigger redial |
| Channel closed | Unavailable | True | Recreate channel |
| Queue not found | NotFound | False | Return error to caller |
| Invalid config | Invalid | False | Fail during Start() |
| Driver stopping | Canceled | False | Graceful exit |
| Publish failed | Unavailable | True | Return error (caller retries) |

---

## Testing Strategy

### Unit Tests

**driver_test.go:**
- Mock amqp.Connection and amqp.Channel
- Test all Driver methods in isolation
- Test lifecycle (Start/Stop)
- Test error conditions

**connection_test.go:**
- Test dial with/without TLS
- Test notification setup
- Test channel creation

**conversion_test.go:**
- Test message header mapping
- Test Ack/Nack callbacks
- Test edge cases (nil headers, etc.)

### Integration Tests

**integration_test.go:**

```go
func TestIntegration_PublishConsume(t *testing.T) {
    if testing.Short() {
        t.Skip("skipping integration test")
    }

    // Start Docker RabbitMQ
    // Create driver
    // DeclareQueue
    // Publish message
    // Attach consumer
    // Verify message received
    // Ack message
}

func TestIntegration_Reconnection(t *testing.T) {
    // Create driver
    // Publish message
    // Kill RabbitMQ connection
    // Verify redial happens
    // Publish message again
    // Verify success
}

func TestIntegration_DLX(t *testing.T) {
    // DeclareQueue with DLX
    // Publish message
    // Nack message (no requeue)
    // Verify message in DLX
}
```

### Docker Setup

```yaml
# tests/docker-compose.yml

services:
  rabbitmq:
    image: rabbitmq:3.12-management-alpine
    ports:
      - "5672:5672"    # AMQP
      - "15672:15672"  # Management UI
    environment:
      RABBITMQ_DEFAULT_USER: guest
      RABBITMQ_DEFAULT_PASS: guest
    healthcheck:
      test: ["CMD", "rabbitmq-diagnostics", "ping"]
      interval: 10s
      timeout: 5s
      retries: 5
```

### Test Execution

```bash
# Unit tests only
go test ./service/queue/amqp/... -short

# Integration tests (requires Docker)
docker-compose -f tests/docker-compose.yml up -d rabbitmq
go test ./service/queue/amqp/... -tags=integration -v

# Cleanup
docker-compose -f tests/docker-compose.yml down -v
```

---

## Implementation Phases

### Phase 1: Foundation (Day 1)

**Tasks:**
1. Create `api/service/queue/amqp/config.go`
   - Config struct
   - TLSConfig with validation
   - InitDefaults() and Validate()
2. Create `api/service/queue/amqp/config_test.go`
   - Test validation
   - Test TLS config building
3. Create `service/queue/amqp/errors.go`
   - AMQPError struct
   - All error constructors
4. Add dependency: `go get github.com/rabbitmq/amqp091-go`

**Acceptance:**
- Config marshaling/unmarshaling works
- TLS config builds with cert files
- All errors implement api/error.Error
- Tests pass

### Phase 2: Connection Management (Days 2-3)

**Tasks:**
1. Create `service/queue/amqp/connection.go`
   - dial() with TLS support
   - Channel creation with publisher confirms
   - Notification setup
2. Create `service/queue/amqp/redial.go`
   - redialLoop() monitoring
   - reconnectWithBackoff() using internal/backoff
3. Write unit tests with mocked connections
4. Test notification handling

**Acceptance:**
- Connection established successfully
- Channels created with prefetch
- Publisher confirms enabled
- Reconnection on failure works
- Backoff intervals correct

### Phase 3: Message Conversion (Day 4)

**Tasks:**
1. Create `service/queue/amqp/convert.go`
   - convertToAMQPPublishing()
   - convertFromAMQPDelivery()
   - Header mapping
   - Ack/Nack callbacks
2. Write conversion tests
3. Test header preservation

**Acceptance:**
- Standard headers mapped correctly
- Trace context preserved
- Ack/Nack callbacks work
- Edge cases handled (nil headers, etc.)

### Phase 4: Topology (Day 5)

**Tasks:**
1. Create `service/queue/amqp/topology.go`
   - DeclareQueue() with attrs mapping
   - buildQueueArgs()
   - declareDLX()
   - GetQueueInfo()
2. Write topology tests
3. Test DLX setup

**Acceptance:**
- Queue declared with options
- DLX created and bound
- Stats retrieved correctly
- Idempotent declaration

### Phase 5: Core Operations (Days 6-7)

**Tasks:**
1. Create `service/queue/amqp/publish.go`
   - Publish() with buffered channel
   - Error handling
2. Create `service/queue/amqp/consume.go`
   - Attach() implementation
   - consumeLoop() with reconnection
   - handleDeliveries()
3. Write operation tests

**Acceptance:**
- Publish reuses channel
- Consume runs continuously
- Reconnection during consume works
- Ack/Nack callbacks execute

### Phase 6: Driver Integration (Day 8)

**Tasks:**
1. Create `service/queue/amqp/driver.go`
   - Driver struct
   - All interface methods
   - Start/Stop lifecycle
   - Queue tracking
2. Write driver tests with mocks
3. Test full lifecycle

**Acceptance:**
- All Driver methods implemented
- Start/Stop respects context
- Queue tracking works
- Integration with connection/redial

### Phase 7: Manager & Bootstrap (Day 9)

**Tasks:**
1. Create `service/queue/amqp/manager.go`
   - Manager struct
   - Add/Update/Delete handlers
   - Config decoding with internal/entry
2. Create `boot/components/queue/amqp.go`
   - Bootstrap component
   - Registry listener registration
3. Update `boot/components/queue/all.go`
4. Update `boot/components/queue/constants.go`
5. Write manager tests

**Acceptance:**
- Manager creates drivers from config
- Hot reload (Update) works
- Bootstrap registers correctly
- Lifecycle integration with supervisor

### Phase 8: Docker & Integration Tests (Days 10-11)

**Tasks:**
1. Update `tests/docker-compose.yml`
   - Add RabbitMQ service
   - Configure healthcheck
2. Create `service/queue/amqp/integration_test.go`
   - TestIntegration_PublishConsume
   - TestIntegration_Reconnection
   - TestIntegration_DLX
   - TestIntegration_QueueStats
   - TestIntegration_MultipleConsumers
   - TestIntegration_TLS
3. Add test helpers
4. Document test setup

**Acceptance:**
- All integration tests pass
- Reconnection tested end-to-end
- DLX messages routed correctly
- TLS connection works
- CI pipeline integration

### Phase 9: Documentation & Polish (Day 12)

**Tasks:**
1. Add GoDoc comments to all public APIs
2. Create usage examples
3. Update main README
4. Run benchmarks
5. Code review cleanup

**Acceptance:**
- All public APIs documented
- Examples compile and run
- Benchmarks show >10k msg/sec
- Code review approved

**Total: 12 days**

---

## Open Questions

### 1. TLS Certificate Paths
**Question:** Should we support environment variable expansion in cert paths?
**Example:** `cert_file: ${CERT_DIR}/client.crt`
**Decision:** TBD

### 2. Exchange Support
**Question:** Should we support publishing to exchanges directly, or queue-only?
**Current:** Queue-only (direct to queue via default exchange)
**Alternative:** Add exchange/routing_key to queue options
**Decision:** TBD

### 3. Prefetch Strategy
**Question:** Global prefetch only or allow per-queue override?
**Current:** Global prefetch in driver config
**Alternative:** Per-queue prefetch via attrs.Attributes
**Decision:** TBD

### 4. Metrics
**Question:** Should we add Prometheus metrics for published/consumed messages?
**Example:** `amqp_messages_published_total{queue="orders"}`
**Decision:** TBD (likely yes, but separate PR)

### 5. Consumer Tags
**Question:** Auto-generated or configurable consumer tags?
**Current:** Auto-generated (empty string = AMQP generates)
**Alternative:** Allow custom tags via options
**Decision:** TBD

---

## Dependencies

### New Dependencies

```
github.com/rabbitmq/amqp091-go  v1.10.0+
```

### Existing Dependencies (Reused)

- `internal/backoff` - Exponential backoff calculator
- `internal/entry` - Config decoding utilities
- `api/error` - Structured error interface
- `api/attrs` - Universal queue options
- `api/queue` - Driver interface
- `api/supervisor` - Service lifecycle

---

## Performance Targets

Based on RoadRunner's proven performance and AMQP protocol capabilities:

| Metric | Target | Rationale |
|--------|--------|-----------|
| Publish throughput | >10,000 msg/sec | Lua workers publishing messages |
| Publish latency (p99) | <1ms | Hot path performance |
| Consume throughput | >5,000 msg/sec | Consumer processing speed |
| Reconnection time | <5s | Network resilience |
| Memory allocation | Zero per publish | After channel warmup |

---

## Acceptance Criteria

### Functional Requirements

- [ ] All Driver interface methods implemented
- [ ] Automatic reconnection on connection loss
- [ ] TLS connections supported
- [ ] Dead letter queues configured
- [ ] Message headers preserved (standard + custom)
- [ ] Queue stats retrieved
- [ ] Graceful shutdown respects Stop context
- [ ] Publisher confirms enabled

### Quality Requirements

- [ ] Unit test coverage >80%
- [ ] All integration tests pass with Docker
- [ ] No race conditions (go test -race)
- [ ] No goroutine leaks
- [ ] Code documented with GoDoc
- [ ] Examples provided

### Performance Requirements

- [ ] Publish throughput >10k msg/sec
- [ ] Zero allocations on publish hot path
- [ ] Reconnection <5s

---

## References

### External Research

1. **RoadRunner AMQP Plugin**
   - Repository: github.com/roadrunner-server/amqp
   - Key files: driver.go, redial.go, listener.go, config.go
   - Patterns adopted: Buffered channels, redial loop, notification monitoring

2. **wagslane/go-rabbitmq**
   - Repository: github.com/wagslane/go-rabbitmq
   - Key insights: Auto-reconnection is primary feature, handler pattern

3. **RabbitMQ Go Tutorial**
   - URL: rabbitmq.com/tutorials/tutorial-one-go.html
   - Key insights: Queue idempotency, defer Close() pattern

### Internal Code

1. **Memory Queue Driver**
   - Path: service/queue/memory/
   - Pattern: driver.go + manager.go + tests

2. **Queue API**
   - Path: api/queue/
   - Files: driver.go, message.go, delivery.go

3. **Error API**
   - Path: api/error/
   - File: error.go

4. **Backoff Calculator**
   - Path: internal/backoff/
   - File: calculator.go

---

## Appendix A: RoadRunner vs Wippy Comparison

| Aspect | RoadRunner | Wippy AMQP |
|--------|-----------|------------|
| Config | Flat structure | Nested with validation |
| Connection | Global pool | Per-driver instance |
| Redial | Custom backoff | internal/backoff |
| Errors | String errors | api/error.Error |
| Lifecycle | Manual | supervisor.Service |
| Message Pool | sync.Pool | queueapi.Message pool |
| Ack/Nack | Inline | Callback closures |
| Channel Strategy | Buffered chan (cap 1) | Same (adopted) |

---

## Appendix B: Alternative Designs Considered

### Alternative 1: Per-Operation Channels (Rejected)

**Pattern:**
```go
func Publish() {
    ch, _ := conn.Channel()
    defer ch.Close()
    ch.Publish(...)
}
```

**Pros:**
- Simple implementation
- No synchronization needed

**Cons:**
- High overhead (channel open/close per publish)
- Doesn't scale to high throughput
- More TCP frames

**Verdict:** Rejected - doesn't meet performance requirements

### Alternative 2: Supervisor-Level Reconnection (Rejected)

**Pattern:**
- No internal redial
- Return error from Start() on connection loss
- Let supervisor restart entire driver

**Pros:**
- Simpler implementation
- Supervisor handles retry logic

**Cons:**
- Lose all state (queue mappings, consumers)
- More expensive (recreate everything)
- Slower recovery

**Verdict:** Rejected - worse user experience

### Alternative 3: Channel Pool (Considered but not adopted)

**Pattern:**
```go
type ChannelPool struct {
    pool chan *amqp.Channel
}
```

**Pros:**
- Supports concurrent publishers
- Classic pattern

**Cons:**
- More complex than needed
- Wippy's Lua execution is single-threaded per worker
- Buffered chan (cap 1) achieves same goal with less code

**Verdict:** Not needed - buffered chan is sufficient

---

**End of Design Document**
