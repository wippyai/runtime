<p align="center">
    <a href="https://wippy.ai" target="_blank">
        <picture>
            <source media="(prefers-color-scheme: dark)" srcset="https://github.com/wippyai/.github/blob/main/logo/wippy-text-dark.svg?raw=true">
            <img width="30%" align="center" src="https://github.com/wippyai/.github/blob/main/logo/wippy-text-light.svg?raw=true" alt="Wippy logo">
        </picture>
    </a>
</p>
<h1 align="center">Adaptive Runtime</h1>
<div align="center">

[![Documentation](https://img.shields.io/badge/docs-wippy.ai-0F6640.svg?style=for-the-badge)][documentation]
[![Go Version](https://img.shields.io/badge/go-1.25+-00ADD8.svg?style=for-the-badge&logo=go)](#installation)
[![License](https://img.shields.io/badge/license-MPL%202.0-blue.svg?style=for-the-badge)](LICENSE)

</div>

Wippy is a process-oriented runtime for building adaptive software systems. It combines Lua 5.3 scripting with Go's performance to create isolated, supervised processes that can be updated without downtime. The entire application state can be exported to a single file for portable deployments.

The architecture enables declarative composition of complex applications that can introspect and modify their own structure at runtime. Designed for AI agents, automation platforms, and systems where components need to evolve based on operational feedback.

## Features

**Process System**
- Erlang-style supervision trees with configurable restart policies
- Process isolation with message passing (no shared state)
- Go-style channels and coroutines for concurrency
- Pluggable schedulers and dispatchers
- Process monitoring and linking for failure propagation
- Location-transparent PIDs across cluster nodes

**Registry**
- Versioned component store with transactional updates
- Hot-reload without service interruption
- Dependency-aware ordering for safe updates
- SQLite-backed history with forward/backward traversal
- Rollback to any previous version

**Security**
- Attribute-based access control (ABAC)
- Expression policies via expr-lang for complex rules
- Token authentication with HMAC signing
- Request-scoped actor and policy enforcement
- Configurable strict mode for security contexts

**Lua Runtime**
- 40+ built-in modules for common operations
- Proto caching for fast script loading
- Function interceptors (retry, metrics, tracing)
- Temporal.io workflow and activity integration
- Contract-based service abstraction
- Native command execution and Docker containers

**Networking**
- HTTP server with dynamic route registration
- WebSocket client and server support
- Middleware: CORS, rate limiting, compression, real IP
- Firewall middleware for endpoint protection
- SSE and chunked transfer encoding

**Storage**
- KV stores with memory and SQL backends
- SQL databases: Postgres, MySQL, SQLite, MSSQL
- Message queues with consumer worker pools
- AWS S3 and cloud storage abstraction
- Environment variable providers (OS, file, memory, composite)

**Observability**
- OpenTelemetry traces and metrics
- Prometheus exporter endpoint
- Structured logging with Zap
- Function-level instrumentation
- HTTP request tracing

**Clustering**
- Gossip-based membership discovery
- Cross-node process messaging via relay
- Distributed topology tracking
- Secret-based cluster authentication

**Extensibility**
- Pluggable command dispatchers
- Custom Lua module registration
- Function interceptor chains
- Event-driven component lifecycle
- WebAssembly runtime (planned)

## Installation

```
git clone https://github.com/wippyai/runtime.git
cd runtime
go build -o wippy ./cmd/runner/
```

## Usage

```
wippy init        # Initialize project with lock file
wippy install     # Install dependencies from lock file
wippy update      # Update dependencies to latest versions
wippy run         # Run application
wippy replace     # Manage local module overrides
```

## Cluster Mode

```
wippy run --cluster --cluster-name=node1 --cluster-join=seed:7946
```

## Configuration

Runtime configuration via `.wippy.yaml`:

```yaml
logger:
  level: debug
  encoding: console

security:
  strict_mode: true

lua:
  proto_cache_size: 60000

otel:
  enabled: true
  endpoint: http://localhost:4318
```

## Requirements

- Go 1.25+

## License

Mozilla Public License 2.0

## Links

- [Documentation][documentation]
- [Issues](https://github.com/wippyai/runtime/issues)

[documentation]: https://docs.wippy.ai


