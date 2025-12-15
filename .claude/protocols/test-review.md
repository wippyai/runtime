# Test Review Protocol v1

## Purpose

This protocol defines how an agent reviews test code for consistency, coverage, and proper organization. The agent must first understand existing test patterns before evaluating tests.

**Goal:** Ensure tests follow codebase conventions, are properly organized, and maintainable.

---

## How This Protocol Is Used

This protocol is given to a child agent with a task like:

```
Review tests for: system/clock
Protocol: protocols/test-review.md
```

The child agent must:
1. Read and understand this entire protocol
2. Study similar component tests first (learn the patterns)
3. Review target package tests against learned patterns
4. Report findings to parent agent (never write to files)

---

## Agent Execution Instructions

### Phase 1: Pattern Learning

**Before reviewing target tests, study similar components.**

1. **Identify component category** - What kind of component is this?
   - Dispatcher? Look at other dispatchers' tests
   - Module? Look at other modules' tests
   - Registry? Look at other registries' tests
   - Engine component? Look at similar engine components

2. **Find 2-3 similar components** and read their test files:
   - How are tests structured?
   - What helper functions exist?
   - How is test setup done?
   - What naming conventions are used?
   - What assertion patterns are used?

3. **Build mental model** of expected patterns before proceeding.

Example:
```
Reviewing: system/clock (dispatcher)
Similar components to study first:
- system/scheduler (dispatcher)
- runtime/lua/evalhost (dispatcher)
- service/http (dispatcher)
```

### Phase 2: Structure Review

Check test organization against the checklist below.

### Phase 3: Content Review

Check test quality and patterns.

### Phase 4: Report

Output findings as structured report. Never write to files.

---

## Review Checklist

### 1. File Pairing

**Every `.go` file should have corresponding `_test.go` file.**

```
package/
  dispatcher.go      → dispatcher_test.go    ✓
  timer.go           → timer_test.go         ✓
  handler.go         → (no test file)        ✗ MISSING
  util.go            → util_test.go          ✓
```

**Check for:**
- [ ] Each implementation file has matching test file
- [ ] Test file tests the corresponding implementation (not random other things)
- [ ] No orphan test files testing code that doesn't exist

**Report missing pairs:**
```
[WARNING] handler.go - MISSING_TEST
No corresponding handler_test.go found
```

### 2. No Scattered Tests

**Tests belong with their implementation, not in random locations.**

**Check for:**
- [ ] No test files in root when testing nested packages
- [ ] No `test/` or `tests/` directories duplicating package structure
- [ ] Integration tests in `integration_test.go` within package (or clearly named)
- [ ] No tests in `_test/` subdirectories

**Bad patterns:**
```
package/
  impl.go
  tests/
    impl_test.go     ✗ Wrong location

project/
  test/
    package/
      impl_test.go   ✗ Scattered
```

**Good pattern:**
```
package/
  impl.go
  impl_test.go       ✓ Adjacent
  integration_test.go ✓ Named clearly
```

### 3. Semantic Grouping

**Related tests should be in same file, not scattered across many small files.**

**Check for:**
- [ ] No test files with single test function
- [ ] Related functionality tested together
- [ ] Logical grouping by feature/behavior

**Bad pattern:**
```
user_create_test.go      (1 test)
user_update_test.go      (1 test)
user_delete_test.go      (1 test)
user_get_test.go         (1 test)
```

**Good pattern:**
```
user_test.go             (all user CRUD tests)
user_integration_test.go (integration scenarios)
```

**Exception:** Large test suites may split by category:
```
builder_test.go          (core builder tests)
builder_select_test.go   (SELECT-specific tests)
builder_insert_test.go   (INSERT-specific tests)
```

### 4. Complex Setup Extraction

**Complicated test setup must be extracted and documented.**

**Check for:**
- [ ] Setup code > 20 lines is extracted to helper function
- [ ] Helper functions have clear names (`setupTestServer`, `newMockDB`)
- [ ] Complex mocks are documented with what they simulate
- [ ] Test fixtures are explained

**Bad pattern:**
```go
func TestHandler(t *testing.T) {
    // 50 lines of setup inline
    ctx := context.Background()
    appCtx := ctxapi.NewAppContext()
    ctx = ctxapi.WithAppContext(ctx, appCtx)
    node := &mockNode{}
    ctx = relay.WithNode(ctx, node)
    reg := scheduler.NewRegistry()
    clockSvc := clock.NewDispatcher()
    clockSvc.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
        reg.Register(id, h)
    })
    // ... more setup

    // actual test
}
```

**Good pattern:**
```go
// setupTestContext creates a context with mock node and app context.
// Used for testing handlers that require relay infrastructure.
func setupTestContext() context.Context {
    appCtx := ctxapi.NewAppContext()
    ctx := ctxapi.WithAppContext(context.Background(), appCtx)
    return relay.WithNode(ctx, &mockNode{})
}

// newTestScheduler creates a scheduler with clock and eval dispatchers.
// Provides Execute() for running processes synchronously in tests.
type testScheduler struct { ... }

func newTestScheduler() *testScheduler { ... }

func TestHandler(t *testing.T) {
    ctx := setupTestContext()
    // actual test
}
```

### 5. Pattern Consistency

**Tests should follow patterns established in similar components.**

**Check for:**
- [ ] Same assertion library used (testify vs stdlib)
- [ ] Same mock patterns (interface mocks vs real implementations)
- [ ] Same table-driven test format where applicable
- [ ] Same cleanup patterns (defer vs t.Cleanup)
- [ ] Same naming conventions (`Test_`, `TestXxx_Yyy`)

**Common patterns to look for:**
```go
// Table-driven tests
func TestParse(t *testing.T) {
    tests := []struct {
        name    string
        input   string
        want    Result
        wantErr bool
    }{
        {"valid input", "abc", Result{...}, false},
        {"empty input", "", Result{}, true},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got, err := Parse(tt.input)
            // assertions
        })
    }
}

// Subtests for categories
func TestClient(t *testing.T) {
    t.Run("Connect", func(t *testing.T) { ... })
    t.Run("Send", func(t *testing.T) { ... })
    t.Run("Receive", func(t *testing.T) { ... })
}
```

### 6. Test Quality

**Tests should be meaningful and maintainable.**

**Check for:**
- [ ] Tests test behavior, not implementation
- [ ] No tests that just call code without assertions
- [ ] Error cases are tested, not just happy path
- [ ] Edge cases covered (nil, empty, zero, max values)
- [ ] No flaky tests (timing-dependent without proper sync)
- [ ] No commented-out tests without explanation
- [ ] Benchmarks use b.ResetTimer() after setup
- [ ] Benchmarks use b.ReportAllocs() when memory matters

**Bad pattern:**
```go
func TestDoSomething(t *testing.T) {
    DoSomething() // no assertion - what is this testing?
}

func TestParse(t *testing.T) {
    result, _ := Parse("input")
    // ignoring error, only testing happy path
}

func BenchmarkParse(b *testing.B) {
    data := expensiveSetup() // setup included in timing!
    for i := 0; i < b.N; i++ {
        Parse(data)
    }
}
```

**Good benchmark pattern:**
```go
func BenchmarkParse(b *testing.B) {
    data := expensiveSetup()
    b.ResetTimer()
    b.ReportAllocs()
    for i := 0; i < b.N; i++ {
        Parse(data)
    }
}
```

### 7. Semantic Test Names

**Test names must describe WHAT is being tested and WHY it's unique.**

**Check for:**
- [ ] No AI slop names ("TestBugFix", "TestFix123", "TestItWorks")
- [ ] No vague names ("TestHandler", "TestParse", "TestNew")
- [ ] Name describes the specific behavior or variance being tested
- [ ] Subtest names in table-driven tests are descriptive

**Bad patterns:**
```go
func TestFix(t *testing.T) { ... }           // what fix?
func TestBugFix(t *testing.T) { ... }        // AI slop
func TestHandler(t *testing.T) { ... }       // which behavior?
func TestParse(t *testing.T) { ... }         // parsing what case?
func TestItWorks(t *testing.T) { ... }       // meaningless
func TestIssue42(t *testing.T) { ... }       // opaque reference
```

**Good patterns:**
```go
func TestParse_ValidJSON(t *testing.T) { ... }
func TestParse_EmptyInput_ReturnsError(t *testing.T) { ... }
func TestParse_MalformedUnicode_HandlesGracefully(t *testing.T) { ... }
func TestHandler_ContextCanceled_CleansUpResources(t *testing.T) { ... }
func TestTimer_Reset_WhileRunning_ExtendsDeadline(t *testing.T) { ... }
```

**When consolidating AI slop tests:**
1. Identify what variance the test actually captures
2. Rename to describe that behavior
3. NEVER lose the test case - the variance matters even if the name was bad

### 8. Duplication and Variance

**Each test should capture a unique variance. Avoid testing same thing multiple ways.**

**Check for:**
- [ ] No duplicate test logic with different names
- [ ] No scattered tests that could be table-driven
- [ ] Each test case has clear, unique purpose
- [ ] When combining tests, ALL variances preserved

**Duplication smell:**
```go
func TestParseInt(t *testing.T) {
    result, _ := Parse("123")
    assert.Equal(t, 123, result)
}

func TestParseInteger(t *testing.T) {  // same test, different name
    result, _ := Parse("456")
    assert.Equal(t, 456, result)
}

func TestParseNumber(t *testing.T) {   // still same variance
    result, _ := Parse("789")
    assert.Equal(t, 789, result)
}
```

**Consolidate to table-driven, preserving ALL variances:**
```go
func TestParse_ValidIntegers(t *testing.T) {
    tests := []struct {
        name  string
        input string
        want  int
    }{
        {"single digit", "5", 5},
        {"multi digit", "123", 123},
        {"large number", "999999", 999999},
        {"with leading zeros", "007", 7},  // unique variance - preserve!
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got, err := Parse(tt.input)
            require.NoError(t, err)
            assert.Equal(t, tt.want, got)
        })
    }
}
```

**Critical rule:** When combining scattered tests:
1. List ALL existing test cases
2. Identify the unique variance each one tests
3. Ensure combined table includes EVERY variance
4. Use descriptive names that explain the variance
5. NEVER drop a test case - if it existed, the variance mattered

### 9. Complexity Assessment

**Agent must assess overall test complexity and recommend simplification.**

**Check for:**
- [ ] Test file complexity proportional to implementation complexity
- [ ] No over-engineered test infrastructure for simple code
- [ ] No under-tested complex code
- [ ] Setup complexity matches actual needs

**Report complexity issues:**
```
[WARNING] handler_test.go - COMPLEXITY
50 lines of test infrastructure for 3 simple tests
Suggestion: Inline simple setup, remove unused helpers

[CRITICAL] scheduler_test.go - COMPLEXITY
Complex scheduler (400 lines) has only 2 basic tests
Suggestion: Add tests for error paths, edge cases, concurrent access
```

### 10. Mock Quality

**Mocks should be minimal and documented.**

**Check for:**
- [ ] Mocks implement only needed methods
- [ ] Mock behavior is documented or obvious
- [ ] No god-mocks that implement everything
- [ ] Mocks are near tests that use them (not in separate package unless shared)

**Good pattern:**
```go
// mockNode implements relay.Node for testing handlers.
// Send() records packages for verification. Other methods are no-ops.
type mockNode struct {
    sent []*relay.Package
}

func (m *mockNode) Send(pkg *relay.Package) error {
    m.sent = append(m.sent, pkg)
    return nil
}
func (m *mockNode) ID() relay.NodeID { return "" }
// ... minimal implementations
```

---

## Output Format

Report findings as structured text to parent agent:

```
## Test Review: {package_path}

### Patterns Learned From
- {similar_package_1}: {key pattern observed}
- {similar_package_2}: {key pattern observed}

### Summary
- Test files: N
- Implementation files without tests: N
- Issues found: N (critical: N, warning: N, info: N)

### Missing Test Coverage
{list of .go files without corresponding _test.go}

### Organization Issues
{scattered tests, poor grouping, etc.}

### Pattern Violations
{deviations from established patterns}

### Setup Complexity
{tests needing extraction/documentation}
```

### Issue Format

```
[SEVERITY] file_test.go:LINE - CATEGORY
Description of issue
Similar component pattern: {how similar component does it}
Suggestion: how to fix
```

Example:
```
[WARNING] handler_test.go - SETUP_COMPLEXITY
Test setup is 45 lines inline, making test hard to read
Similar component pattern: system/scheduler uses newTestScheduler() helper
Suggestion: Extract to setupTestHandler() with documentation

[CRITICAL] - MISSING_TEST
timer.go has no corresponding timer_test.go
Similar component pattern: all system/* packages have paired test files
Suggestion: Create timer_test.go covering timerRegistry methods
```

---

## Severity Levels

| Level | Meaning | Action |
|-------|---------|--------|
| CRITICAL | Missing test coverage | Must add tests |
| WARNING | Pattern violation or poor organization | Should fix |
| INFO | Improvement suggestion | Consider fixing |

---

## Categories

| Category | What it covers |
|----------|----------------|
| MISSING_TEST | No test file for implementation |
| SCATTERED | Tests in wrong location |
| GROUPING | Too many small test files |
| SETUP_COMPLEXITY | Inline setup needs extraction |
| PATTERN_VIOLATION | Deviates from similar component patterns |
| TEST_QUALITY | Meaningless or incomplete tests |
| MOCK_QUALITY | Poor mock design |
| AI_SLOP | Vague or meaningless test names |
| DUPLICATION | Multiple tests covering same variance |
| COMPLEXITY | Test complexity mismatch with implementation |
| BENCHMARK | Incorrect benchmark structure |

---

## What NOT to Do

- Do NOT write fixes to files
- Do NOT review implementation code (use code-review.md for that)
- Do NOT suggest adding tests for generated code
- Do NOT enforce patterns that don't exist in similar components
- Do NOT report on vendored code

---

## Pattern Learning Examples

Before reviewing, find and study similar components:

| If reviewing | Study these first |
|--------------|-------------------|
| system/clock | system/scheduler, other system/* |
| runtime/lua/modules/time | runtime/lua/modules/json, other modules |
| runtime/lua/engine | runtime/lua/evalhost, other engine components |
| service/http | service/sql, other services |
| api/clock | api/dispatcher, other api packages |
| boot/components/* | other boot/components packages |

The goal is to understand "how we do tests here" before judging new tests.
