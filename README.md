<!-- SPDX-License-Identifier: MPL-2.0 -->

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
[![Go Version](https://img.shields.io/badge/go-1.26+-00ADD8.svg?style=for-the-badge&logo=go)](#installation)
[![License](https://img.shields.io/badge/license-MPL%202.0-brightgreen.svg?style=for-the-badge)](LICENSE)

</div>

Wippy is an open-source, actor-model runtime for building complex applications and agent systems - without the stack of infrastructure they normally require.

Durable workflows, queues, scheduling, caching, vector search, and clustering are part of the runtime, so the accidental complexity of wiring a dozen services together - and keeping them consistent, secured, and observable - goes away. Your code runs as isolated, supervised processes, written in typed, linted Lua, that communicate by message passing with no shared state to corrupt; a failure is contained and recovered instead of cascading.

Each process is sandboxed to the capabilities you grant it, your data stays on infrastructure you own, and every change is versioned and reversible. Applications can also evolve at runtime - updated by your team or an AI agent over MCP - without redeploys.

<p align="center">
    <a href="https://github.com/wippyai/app">
        <img src="https://img.shields.io/badge/app%20template-wippyai%2Fapp-FF8C42.svg?style=for-the-badge" alt="App Template">
    </a>
</p>

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
- Vector search in SQLite and Postgres for embeddings and RAG
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

- Bounded Raft consensus core (voters, standbys, gossip-only clients)
- SWIM gossip membership with gossip-driven bootstrap (`bootstrap_expect`)
- Cluster-wide process names with consistency scopes: local, eventual, consistent, strong
- Distributed locks, auto-released when the holder exits
- Process groups: join named groups and broadcast across nodes
- Location-transparent process messaging via relay; encrypted gossip

**Extensibility**

- Pluggable command dispatchers
- Custom Lua module registration
- Function interceptor chains
- Event-driven component lifecycle
- WebAssembly runtime

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
```

## Cluster Mode

Configure clustering in `.wippy.yaml` - point each node at a seed and set the expected initial quorum size:

```yaml
cluster:
  enabled: true
  name: node-1
  membership:
    join_addrs: "node-2:7946,node-3:7946"
  raft:
    bootstrap_expect: 3
```

Any config value can also be set on the command line with repeatable `--set section.path=value`, which take precedence over the file:

```
wippy run --set cluster.enabled=true \
          --set cluster.membership.join_addrs=node-2:7946,node-3:7946 \
          --set cluster.raft.bootstrap_expect=3
```

See the [documentation][documentation] for the cluster model - naming scopes, routing, distributed locks, and process groups.

## Configuration

Runtime configuration via `.wippy.yaml`:

```yaml
version: "1.0"

logger:
  level: info
  encoding: console

logmanager:
  stream_to_events: false

security:
  strict_mode: false

registry:
  enable_history: true
  history_type: memory # memory | sqlite | postgres | nil
  history_path: .wippy/registry.db
  # For postgres history:
  # history_dsn: ${env:WIPPY_REGISTRY_HISTORY_DSN}
  # history_schema: wippy_registry

finder:
  query_cache_size: 1000
  regex_cache_size: 100

profiler:
  enabled: false
  address: localhost:6060

lua:
  proto_cache_size: 60000
  main_cache_size: 10000
  type_system:
    enabled: true
    strict: false
  cache:
    enabled: true
    dir: .wippy/cache/lua
    mode: readwrite # off | readonly | readwrite
    compile:
      enabled: true
    typecheck:
      enabled: true

lsp:
  enabled: false
  address: 127.0.0.1:7777

otel:
  enabled: false
  endpoint: localhost:4318
  protocol: http/protobuf
  traces_enabled: true
  metrics_enabled: false
  http:
    enabled: true
    extract_headers: true
    inject_headers: true
  process:
    enabled: true
    trace_lifecycle: true
  interceptor:
    enabled: true
    order: 100
  queue:
    enabled: true
  temporal:
    enabled: false

metrics:
  interceptor:
    enabled: false
  buffer:
    size: 10000

prometheus:
  enabled: false
  address: ":9090"

modules:
  registry_url: https://hub.wippy.ai

relay:
  node_name: local

supervisor:
  host:
    buffer_size: 1024
    worker_count: 16

cluster:
  enabled: false
  node_name: ""
  membership:
    bind_addr: 0.0.0.0
    bind_port: 7946
    join: ""
    secret_file: ""
    secret: ""
    advertise: ""
  internode:
    bind_addr: 0.0.0.0
    bind_port: 0
    # Optional v2 relay endpoint for upgraded peers. v1 metadata remains the
    # direct bind endpoint, so older peers keep working during rolling upgrades.
    advertise_addr: ""
    advertise_port: 0 # 0 = bind_port; requires advertise_addr
    auto_port: true

extensions:
  enabled: true
  paths: []

override: {}

disable:
  namespaces: []
  entries: []
  meta: {}

shutdown:
  timeout: 30s
```

### Runtime configuration composition

Dependency ranges remain `ns.dependency` registry entries and exact portable
versions remain in `wippy.lock`. For local development, a runtime profile can
replace a locked module with a checkout without changing the lock:

Pass `--config` more than once to compose any set of runtime configuration
files. Files use the same schema and merge from left to right, so later files
override matching leaves while preserving unrelated settings:

```sh
wippy run \
  --config .wippy.yaml \
  --config .wippy.dev.yaml \
  --config config/postgres-history.yaml \
  --config .wippy.workspace.yaml \
  --profile workspace
```

Every explicitly named file must exist. With no `--config`, `.wippy.yaml`
remains the optional default. The first file defines the project directory used
to resolve relative runtime paths. Profiles are applied after all files merge,
in requested order, and CLI overrides such as `--set` are applied last.

Configuration filenames have no reserved meaning. Keep private or
machine-specific files out of version control using the repository's normal
ignore policy. For example, the final file above could contain:

```yaml
version: "1.0"

profiles:
  workspace:
    workspace:
      replacements:
        wippy/runtime: ../runtime
```

Use the same composition for commands that load dependencies:

```sh
wippy update --config .wippy.yaml --config .wippy.workspace.yaml --profile workspace
wippy install --config .wippy.yaml --config .wippy.workspace.yaml --profile workspace
```

The runtime configuration stack is not a publishing input, and the `workspace`
section is never exported in module metadata. A module can also restrict which
non-workspace runtime profiles are published from its `wippy.yaml` manifest:

```yaml
publish:
  profiles:
    include: [production]
  runtime:
    sections: [security, registry, override]
    vars: [public_url]
```

Published runtime configuration is application-owned. The selected sections
and any variables they or the published profiles reference are carried into the
application pack as defaults. Variable references are followed transitively.
Use `publish.runtime.vars` only for intentionally public defaults which are not
otherwise referenced; unlisted variables are not packaged. Publisher
environment references and machine-local `boot`, `extensions`, and `workspace`
sections are rejected.

Existing `replacements:` in `wippy.lock` remain readable for compatibility but
emit a deprecation warning. Move them to any runtime configuration file; new
workspace replacements should not be added to the portable lock.

## Requirements

- Go 1.26+

## License

Mozilla Public License 2.0

## Links

- [Documentation][documentation]
- [Issues](https://github.com/wippyai/runtime/issues)

[documentation]: https://wippy.ai/en/
