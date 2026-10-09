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
go build -o wippy ./cmd/wippy/
```

A plain Go build reports `dev`. Makefile builds prefix the automatic Git
description with `dev-`, including builds from a tagged checkout. Release
builders set the release identity explicitly, for example:

```
make build-wippy-local WIPPY_VERSION=v0.3.43a
```

`wippy version --short` and Lua `system.version()` report the same build
identity. Development and nightly identities do not promise release ordering.

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
  strict_mode: true

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
  type_system:
    enabled: true
    strict: false
    strict_any: false # true: any must be narrowed like unknown (wippy lint --strict-any)
  cache:
    enabled: true
    dir: .wippy/cache/lua
    mode: readwrite # off | readonly | readwrite
    max_bytes: 1073741824
    max_entries: 20000
    prune_interval: 256
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
    identity_key: "" # base64-encoded Ed25519 seed or private key
    identity_key_file: ""
    trusted_peer_keys: {} # node name to base64-encoded Ed25519 public key

override: {}

disable:
  namespaces: []
  entries: []
  meta: {}

shutdown:
  timeout: 30s
```

Native applications may ship a `cmd/app.LuaCacheSeed`. Startup checks its
SHA-256 archive digest, cache schema and linked Lua toolchain identity, then
uses its immutable entries from memory without installing entry files in the
state directory. Metadata and artifacts are decoded when requested; source,
dependency and type-check fingerprints still have to match. A rejected seed or
entry falls back to the persistent cache or compilation. Newly compiled entries
remain in the configured disk cache; cache modes and stage switches still apply.
The bounded archive parser rejects unsafe paths, links, duplicates and oversized
contents. The embedded set is authenticated as a whole, so its immutable files
are not individually rehashed. Native hosts can supply the same read-only layer
through `code.Config.EmbeddedCache`; `lua.cache.embedded` is a typed host override,
not a YAML setting.

Hosts implementing `app.BootLogger` receive deployment, Lua seed, artifact seed
and runtime-start phase records before the event bus exists. Mesh startup and
independent owner-service starts report begin/end/failed records through the
runtime logger. These diagnostics do not change readiness or permissions.

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

Workspace controls are direct children of `workspace`:

```yaml
workspace:
  include_source_dependencies: false # true: add host declarations to a rooted app
  unpack_modules: false             # keep dependency packs as .wapp files
  replacements:
    acme/http: ../http
```

`workspace.unpack_modules` overrides the lock's `options.unpack_modules` setting
without writing it to the lock. When the workspace key is absent, the existing
top-level runtime `options.unpack_modules` setting remains supported with its
previous behavior. If both runtime keys are present, the workspace key wins;
an explicit YAML `workspace.unpack_modules: null` clears the runtime override and
uses the lock setting. Replacements retain their existing path resolution and
profile precedence. These controls use the same configuration composition for
update, install and run, and are never exported as workspace module metadata.

### Adding host dependencies to a published application

A lock with an application root resolves from that application by default;
host source does not add dependencies. To develop a new module alongside a
published application without publishing it first, explicitly enable host
dependency discovery in your runtime configuration:

```yaml
version: "1.0"
workspace:
  include_source_dependencies: true
  replacements:
    local/guide: ../guide
```

Declare the dependency in the host's source directory using the usual
`ns.dependency` entry, for example in `src/_index.yaml`:

```yaml
namespace: host.deps
entries:
  - name: guide
    kind: ns.dependency
    component: local/guide
    version: "*"
```

Use the same runtime configuration or profile for `wippy update` and
`wippy run`. The declaration enrolls the module; the replacement supplies its
local source. A replacement alone does not enroll an otherwise unused module.
An unpublished wildcard replacement uses the resolver's local `0.0.0` version.

The application remains the deployment root, and all dependencies share one
graph. Startup keeps the application's exact locked version, verifies cached
artifacts offline first, and completes the graph before services start. A full
`wippy update` retains its normal latest-compatible selection behavior;
`wippy update local/guide` refreshes declared local dependencies without
upgrading the application. Shared dependencies may need compatible version
changes; conflicting constraints fail resolution rather than creating a
second graph.

This option is off by default and does not change source-only workspaces.
Enabling it adds source scanning and graph validation at startup, not work on
each actor message. Local-only selections require the replacement configuration
and checkout on restart; they are not independently portable published modules.
Removing a host declaration prunes its dependency on the next preparation or
update only if the remaining graph no longer requires it. Disabling the option
stops host discovery; run `wippy update` to reconcile the lock with the application
graph. Neither action uninstalls a module from an already running process.

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
environment references and machine-local `boot` and `workspace`
sections are rejected.

Existing `replacements:` in `wippy.lock` remain readable for compatibility but
emit a deprecation warning. Move them to any runtime configuration file; new
workspace replacements should not be added to the portable lock.

## Requirements

- Go 1.26+

## Optional build features

Default builds and official release binaries exclude Tailscale and Tree-sitter.
SOCKS5/Tor and I2P overlays remain available in the default build. Enable optional
features when building from source:

```sh
make build-wippy WIPPY_FEATURES=tailscale
make build-wippy WIPPY_FEATURES=treesitter
make build-wippy WIPPY_FEATURES="tailscale treesitter"
```

These are Go build tags. For a direct Go build, preserve the standard SQLite
tags and add the requested features:

```sh
CGO_ENABLED=1 go build -tags "fts5 sqlite_vec sqlite_preupdate_hook tailscale treesitter" ./cmd/wippy
```

Use the Makefile targets for a complete executable with its embedded native
confinement helper. Tree-sitter requires CGO. Without the corresponding tag,
`network.tailscale` entries fail with an unsupported-kind error and
`require("treesitter")` is unavailable. The feature choices also apply to
`make run-wippy` and every `build-wippy-*` platform target.

Native Go `.so` extensions are no longer supported. Remove the `extensions`
configuration section; non-empty legacy configuration is rejected at startup.
Lua modules, WebAssembly and statically supplied Go boot components are unaffected.

## License

Mozilla Public License 2.0

## Links

- [Documentation][documentation]
- [Issues](https://github.com/wippyai/runtime/issues)

[documentation]: https://wippy.ai/en/
