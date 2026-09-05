# Wasm Socket Host Migration & Deprecation Guide

## Overview

This document defines the migration path and backward compatibility contract for core WebAssembly socket imports in Wippy actors. The runtime transitions from legacy socket names (`wippy:sock` / `wippy:sock/tcp`) to canonical naming (`socket` / `wippy:runtime/socket@0.1.0`).

Legacy imports remain fully supported for a minimum of 10 released runtime versions to provide a stable transition window for existing guest actors.

---

## Canonical vs. Legacy Identifiers

| Layer | Canonical Identifier | Legacy Identifiers |
|---|---|---|
| **Actor Config (YAML / Manifest)** | `socket` | `wippy:sock`, `wippy:sock/tcp` |
| **Wasm Binary Import (Namespace)** | `wippy:runtime/socket@0.1.0` | `wippy:sock/tcp` |
| **Functions** | `connect`, `send`, `recv`, `close` | `connect`, `send`, `recv`, `close` |

Both YAML aliases (`wippy:sock` and `wippy:sock/tcp`) resolve directly to the canonical `socket` profile in `HostRegistry`.

---

## Deprecation Policy & 10-Release Window

- **Minimum Version Guarantee**: The legacy identifiers (`wippy:sock` and `wippy:sock/tcp`) are guaranteed to remain functional for a minimum of **10 released runtime versions** following the first release that includes this deprecation marker.
- **Draft Release Anchor**: The release anchor is **not yet assigned** in this draft codebase (`first_released_version` is empty). Release notes for the first official release containing this marker must explicitly record the first shipped version before the 10-release support window can be calculated.
- **No Automatic Removal**: Legacy code will **not** be purged or expired automatically after 10 releases. Removal requires a separate, explicit migration decision after that minimum support window.
- **Inspectable Metadata**: Deprecation metadata is programmatically inspectable via `coresocket.LegacyDeprecation` and `HostRegistry.Deprecation(id)`.

---

## Unified Execution & Security Parity

Both canonical and legacy binary namespaces bind to identical underlying handlers and resource management:

1. **Shared Handlers**: Both namespaces execute the identical Go host functions (`Connect`, `Send`, `Recv`, `Close`).
2. **Unified Resource Table**: Both namespaces allocate connection handles into the per-instance `UnifiedTable` using the same resource type (`0x534f434b` / `"SOCK"`).
3. **Identical Quotas & Limits**: Per-instance limits (`MaxOpenSockets`, `SocketTimeoutMS`, shared Preview 2 `SocketBudget`) apply uniformly across both namespaces. Mixed alias handles cannot bypass limits or budgets.
4. **Handle Isolation**: Cross-instance handle access is strictly prevented; instances cannot read, write, or close handles opened by another instance regardless of the namespace used.
5. **Context Cleanliness**: Instance cancellation, timeouts, and instance termination close connections and release budget slots identically across namespaces.

---

## Actionable Warnings & Deduplication

To guide actor authors toward canonical APIs without degrading runtime throughput:

- **Trigger Conditions**:
  - Emitted when an old YAML alias (`wippy:sock` or `wippy:sock/tcp`) is imported in configuration.
  - Emitted when an old binary namespace (`wippy:sock/tcp`) is called by guest code, even if the YAML profile was already configured as `socket`.
- **Deduplication**:
  - Warnings are deduplicated per runtime and alias token.
  - Legacy binary functions employ a lightweight atomic `sync.Once` check per runtime registration, ensuring that high-throughput I/O (`send`, `recv`) experiences zero log spam and zero lock contention after first invocation.
  - Dedup state is lifecycle-scoped to the runtime: no unbounded instance retention or long-lived memory leaks.
- **Canonical Silence**: Modules and configurations using only canonical names produce **no** deprecation warnings.

---

## Ecosystem Context & Python Consumer

- Python consumer draft [MR !1](https://git.wippy.ai/wippy/python-wasm/-/merge_requests/1) transitions the Python WebAssembly toolchain to emit canonical `wippy:runtime/socket@0.1.0` imports.
- Because older Python guest modules remain in active circulation with nothing yet deployed from the MR, the backward compatibility aliases guarantee seamless coexistence of legacy and updated modules on the same host runtime.
