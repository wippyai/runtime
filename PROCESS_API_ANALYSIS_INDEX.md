# Process API Architecture Analysis - Complete Index

This directory contains a comprehensive architectural analysis of the Process API and options management system. The analysis spans 3 detailed documents with 1,527 lines total.

## Documents Overview

### 1. PROCESS_API_ARCHITECTURE.md (680 lines)
**Main architectural reference document**

Provides complete architectural overview across 5 layers:
- **Section 1:** Process Creation APIs & Entry Points
  - API Layer: `Start` and `Launch` structures
  - System Layer: `Manager.Start()` orchestration  
  - Service Layer: `Host.Launch()` implementation
  - Runtime Layer: `LuaProcess.Start()` execution
  
- **Section 2:** Options/Configuration Management
  - Boot-time Configuration (EntryConfig, HostConfig)
  - Runtime Options (untyped context.Pair arrays)
  - Service-specific Options (Retry, OpenTelemetry)
  - How options flow through all layers
  
- **Section 3:** Host Types & Differentiation
  - Host interface hierarchy (Base, Managed, Delegated)
  - Type detection and registration mechanism
  - Current implementations (only Managed exists)
  
- **Section 4:** Data Flow Documentation
  - Configuration bootstrap flow (JSON → EntryConfig → Host)
  - Runtime options flow (Start.Context → FrameContext → Runtime)
  - Config duplication analysis
  
- **Section 5:** Current Pain Points (5 key issues)
  - Adding process-level options (scattered changes)
  - Interceptor options duplication
  - Context pair validation (silent failures)
  - Host configuration scattered across layers
  - Process pool API lacks granularity
  
- **Section 6:** Architectural Patterns
  - Event-driven registration for hosts/prototypes
  - Context as options container (inheritance model)
  - Process pool abstraction design
  
- **Section 7-9:** Summary, recommendations, and file reference map

**Best for:** Understanding overall architecture and identifying what changes where

---

### 2. PROCESS_OPTIONS_FLOW.md (383 lines)
**Visual guide to data flow and problem areas**

Step-by-step visual breakdown of process creation and options handling:

- **Section 1:** Process Startup Sequence
  - ASCII flow diagram from user code → Manager → Host → Runtime
  
- **Section 2:** Context Preparation in Service Layer
  - Shows context inheritance and pair application
  
- **Section 3:** Options Visibility Across Layers
  - Table showing what each layer sees/modifies
  
- **Section 4:** Configuration vs Runtime Options
  - Visual matrix of different option types
  
- **Section 5:** Option Access Points
  - Where in process lifecycle options can be accessed
  
- **Section 6:** Option Addition Impact Map
  - Concrete example: adding "timeout" option
  - Shows impact on each layer
  
- **Section 7:** The "Pair" Array Problem
  - Detailed analysis of untyped options design
  - Current problems vs ideal approach
  
- **Section 8:** Current vs Ideal Configuration Hierarchy
  - Fragmented current state
  - Unified ideal state proposal

**Best for:** Understanding data flow visually and identifying specific problem areas

---

### 3. PROCESS_COUPLING_ANALYSIS.md (464 lines)
**Detailed coupling points and impact analysis**

Identifies 6 critical coupling points with code examples:

1. **Manager ↔ Host Interface Coupling**
   - Manager has different paths for Managed vs Delegated hosts
   - Delegated host interface signature mismatch
   
2. **Host ↔ ProcessPool Configuration Coupling**
   - Pool config fixed at creation time
   - No way to query or adjust pool utilization
   
3. **FrameContext ↔ Custom Options Coupling**
   - No validation of context pairs
   - Blind copy of options to context
   
4. **LuaProcess ↔ State Coupling**
   - Process directly accesses State internals
   - Hard to inject different option handling
   
5. **Interceptor Config ↔ Options Duplication**
   - Config and Options types have overlapping fields
   - Duplication instead of inheritance
   
6. **ProcessPool ↔ Worker Execution Coupling**
   - Pool tightly coupled to Process interface
   - Hard to support different execution models

Each includes:
- Code examples showing the coupling
- What problems it creates
- Impact on future features

Also includes:
- Dependency direction analysis
- Two concrete scenarios showing coupling impact when adding options
- Recommendations (short, medium, long term) to reduce coupling

**Best for:** Understanding why changes are difficult and what's tightly coupled

---

## Key Findings Summary

| Aspect | Finding | Impact |
|--------|---------|--------|
| **Options Schema** | No schema at API level (untyped pairs) | Type unsafe, silent failures |
| **Config Pattern** | Mixed bootstrap/runtime configurations | Confusing, duplicated |
| **Options Flow** | Via untyped context.Pair arrays | No validation, discovery |
| **Host Types** | 2 interfaces defined, 1 implemented | Delegated hosts untested |
| **Layer Coupling** | Multiple tight couplings | Hard to extend, scattered changes |
| **Validation** | No schema-based validation | Invalid options ignored silently |
| **Documentation** | Scattered across multiple files | Difficult to maintain |

---

## Critical Insights

### 1. Options Flow is Untyped
```
Start.Context: []context.Pair  ← No schema
    ↓ (passed unchanged)
Launch.Context: []context.Pair ← No schema
    ↓ (applied to FrameContext)
FrameContext values           ← No validation
    ↓ (accessed in Runtime)
Lua context.Get()             ← At application time
```

**Consequence:** Invalid options silently ignored, typos cause subtle bugs.

### 2. Configuration is Scattered
- Host config: `api/service/host/config.go`
- Used in: `service/host/host.go`, `service/host/pool.go`
- Referenced in: `service/host/factory.go`
- Bootstrap: `boot/components/service/service/host.go`

**Consequence:** Changes affect 4+ files, hard to find all usages.

### 3. No Options Registry
Currently:
- Valid options not documented in code
- Discovery requires reading multiple layers
- Validation happens nowhere (silently ignored)

Ideal:
- Central registry of valid options
- Compile-time/boot-time validation
- Discovery mechanism for tools

### 4. Interceptor Pattern is Duplicated
```go
// Config (bootstrap)
type Config struct {
    MaxAttempts int
}

// Options (runtime) - same field!
type Options struct {
    MaxAttempts int
    BackoffMs   int
}
```

**Consequence:** Adding field requires changes in 2+ places.

### 5. Delegated Hosts Are Incompletely Designed
- Interface defined but signature different from Managed
- Manager doesn't pass Source/Prototype ID
- No delegated host implementations in codebase
- Hard to support remote process spawning

---

## Recommended Next Steps

### Immediate (Documentation)
1. Create PROCESS_OPTIONS_SCHEMA.md listing all valid context.Pair keys
2. Document each option's:
   - Meaning and expected type
   - Which layer(s) use it
   - Default behavior if missing

### Short Term (No Breaking Changes)
1. Add validation in `Host.prepareContext()` for known options
2. Consolidate interceptor Config/Options using composition
3. Add ProcessPool.GetStatus() for pool visibility

### Medium Term (Backwards Compatible)
1. Create typed ProcessOptions struct alongside context.Pair
2. Start migrating new code to use ProcessOptions
3. Create OptionsRegistry for validation and discovery
4. Fix Delegated.Launch() signature to include Source

### Long Term (Major Refactor)
1. Move to typed ProcessOptions throughout codebase
2. Centralized options validation and schema
3. Support multiple host types with unified interface
4. Enable dynamic pool scaling and metrics

---

## File Reference Quick Lookup

### Core Process API
- `api/process/process.go` - Start, Launch, Process, Host interfaces
- `api/process/context.go` - Lifecycle callbacks (OnStart, OnComplete)
- `api/runtime/task.go` - Task with untyped Options

### System Layer  
- `system/process/manager.go` - Start orchestration and host dispatch
- `system/process/host_registry.go` - Host lookup and type detection
- `system/process/prototype_registry.go` - Process prototype factory

### Service Layer (Host Implementation)
- `service/host/host.go` - Managed host, context preparation (❌ no validation)
- `service/host/pool.go` - ProcessPool with worker goroutines
- `service/host/factory.go` - Host and pool factories
- `api/service/host/config.go` - EntryConfig and Config structures

### Runtime Layer (Lua Process)
- `runtime/lua/component/process/process.go` - LuaProcess implementation
- `runtime/lua/component/process/state.go` - Process state management

### Bootstrap Layer
- `boot/components/service/service/host.go` - Host component bootstrap
- `boot/components/service/service/all.go` - Interceptor registration list

### Configuration Examples
- `api/service/interceptor/retry/config.go` - Config/Options duplication example
- `api/service/interceptor/otel/config.go` - OpenTelemetry options
- `runtime/lua/code/build_options.go` - BuildOptions (different pattern)

---

## How to Use These Documents

**If you want to...**

- **Understand overall architecture**: Start with PROCESS_API_ARCHITECTURE.md Sections 1-3
- **Trace how options flow**: Read PROCESS_OPTIONS_FLOW.md Sections 1-3
- **See what's tightly coupled**: Read PROCESS_COUPLING_ANALYSIS.md
- **Find specific code locations**: See File Reference Map in PROCESS_API_ARCHITECTURE.md Section 9
- **Add a new process option**: Follow Scenario in PROCESS_COUPLING_ANALYSIS.md or PROCESS_OPTIONS_FLOW.md Section 6
- **Understand pain points**: Read PROCESS_API_ARCHITECTURE.md Section 5 and PROCESS_COUPLING_ANALYSIS.md

---

## Document Statistics

| Document | Lines | Focus |
|----------|-------|-------|
| PROCESS_API_ARCHITECTURE.md | 680 | Comprehensive reference, all 5 layers |
| PROCESS_OPTIONS_FLOW.md | 383 | Visual flow diagrams, data movement |
| PROCESS_COUPLING_ANALYSIS.md | 464 | Tight coupling points with code |
| **Total** | **1,527** | **Complete architectural view** |

---

## Contributing to Analysis

These documents capture the current state as of the "feature/scorched-earth" branch. When making architectural changes:

1. Update relevant sections in these docs
2. Keep file path references current
3. Update pain points when addressed
4. Add new coupling points if discovered
5. Track recommendations as implemented

---

Last updated: 2025-11-15
Branch: feature/scorched-earth
Analysis scope: Process API architecture and options management
