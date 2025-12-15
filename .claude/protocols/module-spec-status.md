# Module Spec Protocol Status

This file tracks the status of all Lua module specifications. It must be kept up-to-date as specs are created or updated.

---

## How To Use This Protocol

### Creating/Updating a Spec

1. Select a module from the list below that needs a spec (status: `needs-spec` or `needs-update`)
2. Use the spec protocol: `protocols/module-spec.md`
3. Follow all phases in the spec protocol exactly
4. After completion, run verification (see below)
5. Update this file with new status

### Verification Process

Every spec MUST be verified by a secondary agent before being marked as `verified`. The verification agent should:

1. Read the spec without looking at source code
2. Attempt to mentally "use" the module based only on the spec
3. Check for:
   - Can I load the module correctly?
   - Can I call every function with correct arguments?
   - Can I access all constants with full paths?
   - Can I handle all return values and errors?
   - Are all dependencies documented?
4. Then compare against source code for accuracy
5. Report any gaps or issues

Only mark a spec as `verified` after this process completes with no issues.

### Invoking Agents

**To create a spec:**
```
Create spec for module: {module_name}
Module path: runtime/lua/modules/{module_name}/
Protocol: protocols/module-spec.md
Status file: protocols/module-spec-status.md (update when done)
```

**To verify a spec:**
```
Verify spec for module: {module_name}
Spec path: runtime/lua/modules/{module_name}/spec.md
Protocol: protocols/module-spec.md (use validation checklist)
Do NOT look at source code first - test spec usability, then verify against source
```

---

## Core Dependencies

These are shared types used by multiple modules. Specs for these MUST exist before dependent modules can have complete specs.

**Note:** These are globally available - no `require()` needed. Omit Loading section in their specs.

| Component | Location | Spec Location | Status | Notes |
|-----------|----------|---------------|--------|-------|
| engine | runtime/lua/engine/ | runtime/lua/engine/spec.md | `draft` | Channels, coroutines, primitives |
| errors | runtime/lua/modules/errors.md | (exists) | `exists` | Error constants and methods reference |
| payload | runtime/lua/modules/payload/ | runtime/lua/modules/payload/spec.md | `draft` | Data transcoding, globally available |
| process | runtime/lua/modules/process/ | runtime/lua/modules/process/spec.md | `draft` | Process management, globally available |

---

## Module Status

Status values:
- `needs-spec` - No spec exists
- `needs-update` - Spec exists but doesn't follow protocol or is incomplete
- `draft` - Spec created, awaiting verification
- `verified` - Spec verified by secondary agent
- `reference` - The reference spec (base64)

### Simple Modules (< 500 LOC)

| Module | LOC | Yields | Status | Last Updated | Notes |
|--------|-----|--------|--------|--------------|-------|
| base64 | 277 | - | `reference` | 2024-12-13 | Reference implementation |
| env | 342 | - | `draft` | 2025-12-13 | |
| yaml | 352 | - | `draft` | 2025-12-13 | |
| payload | 383 | - | `draft` | 2025-12-13 | Core dependency |
| ctx | 408 | - | `draft` | 2025-12-13 | |
| io | 433 | - | `draft` | 2025-12-13 | |
| metrics | 478 | - | `draft` | 2025-12-13 | |

### Medium Modules (500-1500 LOC)

| Module | LOC | Yields | Status | Last Updated | Notes |
|--------|-----|--------|--------|--------------|-------|
| template | 518 | - | `draft` | 2025-12-13 | |
| logger | 530 | - | `draft` | 2025-12-13 | |
| future | 634 | - | `draft` | 2025-12-13 | Type returned by funcs.async |
| ostime | 661 | - | `draft` | 2025-12-13 | Globally available |
| uuid | 669 | - | `draft` | 2025-12-13 | |
| expr | 690 | - | `draft` | 2025-12-13 | |
| events | 733 | Y | `draft` | 2025-12-13 | Has yields, uses channels |
| hash | 878 | - | `needs-update` | - | Old format |
| system | 903 | - | `draft` | 2025-12-13 | |
| excel | 943 | - | `draft` | 2025-12-13 | |
| store | 1007 | Y | `draft` | 2025-12-13 | Has yields |
| queue | 1042 | - | `draft` | 2025-12-13 | |
| compress | 1147 | - | `needs-update` | - | Old format |
| html | 1193 | - | `needs-update` | - | Old format |
| websocket | 1222 | Y | `draft` | 2025-12-13 | Uses channels |
| exec | 1267 | Y | `draft` | 2025-12-13 | Has yields |
| cloudstorage | 1326 | Y | `draft` | 2025-12-13 | Has yields |
| httpclient | 1391 | Y | `draft` | 2025-12-13 | Has yields |
| security | 1404 | Y | `draft` | 2025-12-13 | Multiple files, has yields |
| stream | 1410 | Y | `needs-spec` | - | Has yields, uses channels |
| crypto | 1424 | - | `draft` | 2025-12-13 | Updated to new protocol |

### Complex Modules (> 1500 LOC)

| Module | LOC | Yields | Status | Last Updated | Notes |
|--------|-----|--------|--------|--------------|-------|
| text | 1605 | - | `draft` | 2025-12-13 | Has submodules |
| json | 1606 | - | `needs-update` | - | Old format |
| funcs | 1675 | Y | `draft` | 2025-12-13 | Has yields |
| fs | 1797 | - | `draft` | 2025-12-13 | Multiple files |
| eval | 1836 | Y | `draft` | 2025-12-13 | Has yields, complex |
| contract | 1936 | Y | `draft` | 2025-12-13 | Has yields |
| process | 1949 | - | `draft` | 2025-12-13 | Core module, globally available |
| http | 2464 | - | `draft` | 2025-12-13 | Multiple files |
| treesitter | 2962 | - | `draft` | 2025-12-13 | Many types |
| time | 2983 | Y | `draft` | 2025-12-13 | Has yields, uses channels |
| registry | 3556 | - | `draft` | 2025-12-13 | 20 files, complex |
| sql | 6764 | Y | `draft` | 2025-12-13 | 29 files, most complex |

---

## Priority Order

Recommended order for spec creation:

### Phase 1: Core Dependencies (globally available, no Loading section)
1. `engine` - Channels, coroutines, primitives (runtime/lua/engine/spec.md)
2. `payload` - Core data type
3. `process` - Process management

### Phase 2: Simple Modules (practice, fast wins)
4. `env`
5. `ctx`
6. `io`
7. `template`
8. `logger`

### Phase 3: Medium Modules with Yields
9. `events` - Uses channels
10. `store`
11. `websocket` - Uses channels (update existing)
12. `stream` - Uses channels
13. `httpclient`

### Phase 4: Complex Modules
14. `sql` - Most complex but critical
15. Remaining modules by priority

---

## Changelog

| Date | Module | Action | Agent |
|------|--------|--------|-------|
| 2024-12-13 | base64 | Created as reference | opus |
| 2024-12-13 | - | Protocol created | opus |
| 2025-12-13 | engine | Created spec for channels, coroutine.spawn | opus |
| 2025-12-13 | ctx | Created spec | sonnet |
| 2025-12-13 | io | Created spec | sonnet |
| 2025-12-13 | template | Created spec | sonnet |
| 2025-12-13 | store | Created spec | sonnet |
| 2025-12-13 | env | Created spec | sonnet |
| 2025-12-13 | events | Created spec | sonnet |
| 2025-12-13 | websocket | Updated spec to new protocol | sonnet |
| 2025-12-13 | payload | Updated spec to new protocol | opus |
| 2025-12-13 | httpclient | Created spec | sonnet |
| 2025-12-13 | process | Created spec | opus |
| 2025-12-13 | expr | Created spec | sonnet |
| 2025-12-13 | queue | Created spec | sonnet |
| 2025-12-13 | uuid | Updated spec to new protocol | sonnet |
| 2025-12-13 | ostime | Created spec | sonnet |
| 2025-12-13 | system | Created spec | sonnet |
| 2025-12-13 | hash | Updated spec to new protocol | sonnet |
| 2025-12-13 | crypto | Updated spec to new protocol | sonnet |
| 2025-12-13 | sql | Created spec | sonnet |
| 2025-12-13 | text | Updated spec to new protocol | sonnet |
| 2025-12-13 | excel | Updated spec to new protocol | sonnet |
| 2025-12-13 | cloudstorage | Created spec | sonnet |
| 2025-12-13 | funcs | Created spec | sonnet |
| 2025-12-13 | metrics | Updated spec to new protocol | sonnet |
| 2025-12-13 | http | Created spec | sonnet |
| 2025-12-13 | eval | Created spec | sonnet |
| 2025-12-13 | fs | Created spec | sonnet |
| 2025-12-13 | time | Created spec | sonnet |
| 2025-12-13 | security | Created spec | sonnet |
| 2025-12-13 | registry | Created spec | sonnet |
| 2025-12-13 | contract | Created spec | sonnet |
| 2025-12-13 | treesitter | Updated spec to new protocol | sonnet |

---

## Notes

- Remaining specs to update: compress, html, json, stream
- base64 is the reference implementation - use it as the model
- Always update this file after completing work on any module
- Dependencies section is critical - verify all dependent types are documented
