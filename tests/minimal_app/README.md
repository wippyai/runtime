# Minimal Test App

Minimal test structure to verify `.wippy` and `replacements` directory exclusion functionality.

## Size
- **minimal_app**: ~60KB
- **app** (full version): ~2.4MB
- **Savings**: ~40x smaller

## Structure

```
minimal_app/
├── wippy.lock              # Lock file with 0 dependencies + 1 replacement
├── system.yaml             # Minimal system configuration
├── src/                    # Source code
│   ├── _index.yaml
│   └── test_handler.lua
├── replacements/           # Local module (should be excluded)
│   └── test_module/
│       ├── _index.yaml     # Should NOT be loaded
│       └── should_not_load.lua
├── data/
│   └── test.db
└── public/
    └── index.html
```

## Quick Test

```bash
cd tests/minimal_app
./test.sh
```

## Manual Tests

### 1. Test `update`:
```bash
../../dist/runner-linux-amd64 update -l wippy.lock -v
```

**Expected result:**
- ✅ Filters `.wippy`
- ✅ Filters `replacements/test_module`
- ✅ Loads `app:test_handler`
- ❌ Does NOT load `test.should.not.load:test_entry`

### 2. Test `run`:
```bash
timeout 2 ../../dist/runner-linux-amd64 run -l wippy.lock -v
```

**Expected result:**
- ✅ Filtering works
- ✅ Application starts
- ❌ `should.not.load` does not appear in logs

### 3. Verify exclusion:
```bash
grep -i "should.not.load" <(../../dist/runner-linux-amd64 run -l wippy.lock 2>&1) \
  || echo "✅ replacements directory correctly excluded"
```

## What is tested

1. **`.wippy/` exclusion** during src scanning
2. **`replacements/` exclusion** from lock file
3. **Loading files from `src/`** works correctly
4. **Minimal configuration** is sufficient for operation

## Usage

This structure is ideal for:
- ✅ Unit tests
- ✅ CI/CD pipeline
- ✅ Quick functionality verification
- ✅ Minimal debugging environment

