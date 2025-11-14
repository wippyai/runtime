# Entry Unmarshal Inventory - Service Layer

## Overview
Inventory of all locations where `entry.Data` is unpacked in the service layer, categorized by approach.

## Pattern Categories

### ✅ Already Using `DecodeAndInitConfig`
Services that already use the standardized helper function.

| Service | File | Lines | Config Type | Notes |
|---------|------|-------|-------------|-------|
| aws/s3 | `service/aws/s3/manager.go` | 175 | `services3.Config` | In `set()` method |
| aws/config | `service/aws/config/manager.go` | 63, 113 | `serviceaws.Config` | Add & Update |
| memstore | `service/memstore/manager.go` | 56, 110 | `memstore.MemoryConfig` | Add & Update |
| sqlstore | `service/sqlstore/manager.go` | 55, 112 | `sqlstore.SQLConfig` | Add & Update |
| tokenstore | `service/tokenstore/manager.go` | 63, 106 | `tokenstore.Config` | Add & Update |
| sql | `service/sql/manager.go` | 114, 152, 171, 190 | `config.DBConfig`, `config.SQLiteConfig` | Multiple kinds |
| terminal | `service/terminal/manager.go` | 61, 85 | `api.HostConfig` | Add & Update |
| supervisor | `service/supervisor/manager.go` | 102, 197 | `processapi.ServiceConfig` | Add & Update |

**Total: 8 services, 16+ usages**

---

### 🔄 Manual Unmarshal (Candidates for Migration)
Services using direct `dtt.Unmarshal` that could be migrated to `DecodeAndInitConfig`.

#### env/manager.go (5 usages)
| Method | Line | Config Type | Has Meta? | Notes |
|--------|------|-------------|-----------|-------|
| `handleMemoryStorageAdd` | 55 | `envsvc.MemoryStorageConfig` | ✅ | Has `Meta registry.Metadata` field |
| `handleFileStorageAdd` | 77 | `envsvc.FileStorageConfig` | ✅ | Has `Meta registry.Metadata` field |
| `handleOSStorageAdd` | 99 | `envsvc.OSStorageConfig` | ✅ | Has `Meta registry.Metadata` field |
| `handleRouterStorageAdd` | 121 | `envsvc.RouterStorageConfig` | ✅ | Has `Meta registry.Metadata` field |
| `handleVariableAdd` | 150, 183 | `env.Variable` | ✅ | Has `Meta registry.Metadata`, manually sets `variable.ID = entry.ID` |

**Migration Priority: HIGH** - All configs have Meta fields

#### di/manager.go (2 usages)
| Method | Line | Config Type | Has Meta? | Notes |
|--------|------|-------------|-----------|-------|
| `decodeDefinition` | 437 | `apidi.DefinitionConfig` | ✅ | **Already manually inits Meta**: `cfg := &apidi.DefinitionConfig{Meta: entry.Meta}` |
| `decodeBinding` | 448 | `apidi.BindingConfig` | ✅ | **Already manually inits Meta**: `cfg := &apidi.BindingConfig{Meta: entry.Meta}` |

**Migration Priority: MEDIUM** - Already handles Meta manually, could simplify

#### directory/manager.go (2 usages)
| Method | Line | Config Type | Has Meta? | Notes |
|--------|------|-------------|-----------|-------|
| `Add` | 46 | `dirapi.Config` | ❓ | Need to check config structure |
| `Update` | 76 | `dirapi.Config` | ❓ | Need to check config structure |

**Migration Priority: MEDIUM**

#### host/manager.go (1 usage)
| Method | Line | Config Type | Has Meta? | Notes |
|--------|------|-------------|-----------|-------|
| `Add` | 56 | `host.EntryConfig` | ❓ | Need to check config structure |

**Migration Priority: LOW**

#### exec/manager.go (2 usages)
| Method | Line | Config Type | Has Meta? | Notes |
|--------|------|-------------|-----------|-------|
| `Add` | 76 | `exec.NativeExecutorConfig` | ❓ | Need to check config structure |
| `Update` | 127 | `exec.NativeExecutorConfig` | ❓ | Need to check config structure |

**Migration Priority: MEDIUM**

#### policy/factory.go (1 usage)
| Method | Line | Config Type | Has Meta? | Notes |
|--------|------|-------------|-----------|-------|
| `CreatePolicyEntry` | 34 | `policy.Config` | ❓ | Need to check config structure |

**Migration Priority: LOW**

---

### 🔧 Custom Unmarshal Patterns (Special Cases)

#### http/manager.go (1 usage)
| Method | Line | Config Type | Notes |
|--------|------|-------------|-------|
| `decodeEntity[T]` | 619 | Generic `T` | **Already has custom implementation similar to DecodeAndInitConfig** - checks for `SetMeta` interface at line 624 |

**Migration Priority: LOW** - Already implements the pattern correctly

#### template/manager.go (1 usage)
| Method | Line | Config Type | Notes |
|--------|------|-------------|-------|
| `decodeEntity[T]` | 547 | Generic `T` | Custom generic helper, handles Meta manually after unmarshal (lines 131-133, 192-194) |

**Migration Priority: MEDIUM** - Could unify with standard approach

---

### 🚫 Non-Entry Unmarshal (Out of Scope)

#### tokenstore/store.go (1 usage)
| Method | Line | Usage | Notes |
|--------|------|-------|-------|
| `VerifyToken` | 210 | `s.dtt.Unmarshal(value, &data)` | Unmarshaling stored token data, NOT registry entry |

**Migration: N/A** - Not entry-based unmarshal

#### template/set.go (1 usage)
| Method | Line | Usage | Notes |
|--------|------|-------|-------|
| `RenderPayload` | 195 | `s.dtt.Unmarshal(data, &vars)` | Template data rendering, NOT registry entry |

**Migration: N/A** - Not entry-based unmarshal

---

## Summary Statistics

| Category | Count | Files |
|----------|-------|-------|
| Using DecodeAndInitConfig | 16+ usages | 8 services |
| Manual Unmarshal (migratable) | 13 usages | 6 services |
| Custom patterns (review) | 2 usages | 2 services |
| Non-entry unmarshal | 2 usages | 2 services |
| **Total entry-based** | **31+ usages** | **16 services** |

## Migration Recommendations

### High Priority (Has Meta fields)
1. **service/env/manager.go** - 5 usages, all configs have Meta fields
   - `MemoryStorageConfig`, `FileStorageConfig`, `OSStorageConfig`, `RouterStorageConfig`
   - `Variable` needs special handling for `variable.ID = entry.ID`

### Medium Priority
2. **service/di/manager.go** - Already manually handles Meta, can simplify
3. **service/directory/manager.go** - 2 usages
4. **service/exec/manager.go** - 2 usages
5. **service/template/manager.go** - Custom generic helper, could unify

### Low Priority
6. **service/host/manager.go** - 1 usage
7. **service/policy/factory.go** - 1 usage

### Review Only
- **service/http/manager.go** - Already implements pattern correctly
- Non-entry unmarshal cases - keep as-is

## Next Steps

1. Verify Meta field presence in configs marked with ❓
2. Create migration plan for high-priority services
3. Consider adding Validate/InitDefaults to configs that could benefit
4. Update configs to implement SetMeta interface where needed
