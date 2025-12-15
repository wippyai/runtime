# Recombination Protocol v1

## Purpose

This protocol defines how to analyze component composition through dialectical synthesis. An agent reviews a component, generates multiple critical perspectives, and synthesizes them into **recommendations only**.

**Goal:** Produce a detailed analysis with recommendations. **DO NOT implement changes.**

**Protocol consumer:** Agent tasked with reviewing a component and producing recommendations.

**CRITICAL:** This protocol produces a REPORT with recommendations. The agent MUST NOT make any code changes. Only analyze and recommend. The user will decide what to implement.

---

## When to Use

- Component feels over-engineered or has accumulated complexity
- Redundancy suspected across related components
- Industry alignment check needed (API patterns, naming, error handling)
- Before major feature addition to ensure solid foundation
- Performance-critical code that may have unnecessary abstraction layers

---

## Input

```
Component to review: {path or description}
Context: {runtime/library/CLI/API}
Performance priority: {critical/moderate/low}
```

---

## Phase 1: Deep Understanding

Before generating opinions, fully understand the component.

### 1.1 Read Everything

- All source files in the component
- Tests (unit, integration)
- Usages across the codebase
- Related components it interacts with
- Any existing documentation

### 1.2 Build Mental Model

Answer these questions:

| Question | Answer |
|----------|--------|
| What is the component's single responsibility? | |
| What are its public interfaces? | |
| What are its dependencies? | |
| What state does it manage? | |
| How is it tested? | |
| Where is it used? | |

### 1.3 Identify Boundaries

- Input boundaries (what comes in)
- Output boundaries (what goes out)
- Error boundaries (what can fail)
- Async boundaries (what yields/blocks)

---

## Phase 2: Generate Five Perspectives

Generate exactly 5 distinct critical perspectives on the component. Each perspective must:
- Come from a different angle
- Be genuinely critical (not praise)
- Identify specific issues with code references
- Propose concrete changes

### Required Perspectives

| # | Perspective | Focus |
|---|-------------|-------|
| 1 | **Complexity Critic** | Over-engineering, unnecessary abstractions, premature optimization |
| 2 | **Redundancy Hunter** | Code duplication, overlapping responsibilities, unused code |
| 3 | **Industry Standards Auditor** | Common patterns others expect, API conventions, naming |
| 4 | **Testability Advocate** | Isolation, mockability, dependency injection, state management |
| 5 | **Performance Analyst** | Allocation patterns, hot paths, caching opportunities, runtime overhead |

### Perspective Template

For each perspective, document:

```markdown
### Perspective N: {Name}

**Issues Found:**
1. {issue with file:line reference}
2. {issue with file:line reference}
...

**Industry Context:**
{What do similar components in the industry do differently?}

**Proposed Changes:**
1. {specific refactoring}
2. {specific refactoring}
...

**Trade-offs:**
- Pro: {benefit}
- Con: {cost}
```

---

## Phase 3: Select Top Three

From the 5 perspectives, select the 3 most impactful for this specific component.

Selection criteria:
- **Impact:** How much would addressing this improve the component?
- **Feasibility:** Can this be done without major architectural changes?
- **Alignment:** Does this serve the component's primary purpose?

Document selection:

```markdown
## Selected Perspectives

| Rank | Perspective | Reason for Selection |
|------|-------------|---------------------|
| 1 | {name} | {why this is most impactful} |
| 2 | {name} | {why this ranks second} |
| 3 | {name} | {why this ranks third} |

**Excluded:**
- {perspective}: {why excluded}
- {perspective}: {why excluded}
```

---

## Phase 4: Cross-Critique

The 3 selected perspectives must critique each other to find conflicts and synergies.

### 4.1 Conflict Analysis

For each pair of perspectives, identify conflicts:

```markdown
### Conflicts

**{Perspective A} vs {Perspective B}:**
- A wants: {X}
- B wants: {Y}
- Conflict: {how they contradict}
- Resolution: {which should win and why}

**{Perspective A} vs {Perspective C}:**
...

**{Perspective B} vs {Perspective C}:**
...
```

### 4.2 Synergy Analysis

Identify where perspectives reinforce each other:

```markdown
### Synergies

**{Perspective A} + {Perspective B}:**
- A's change {X} also helps B's goal of {Y}
- Combined approach: {unified solution}

...
```

---

## Phase 5: Synthesis

Produce a unified recommendation that resolves conflicts and leverages synergies.

### 5.1 Core Thesis

State the single most important insight:

```markdown
## Synthesis

**Core Insight:**
{One sentence capturing the fundamental improvement needed}
```

### 5.2 Prioritized Refactoring Plan

```markdown
### Refactoring Plan

| Priority | Change | Rationale | Risk |
|----------|--------|-----------|------|
| P0 | {change} | {from which perspectives} | {what could go wrong} |
| P1 | {change} | {from which perspectives} | {what could go wrong} |
| P2 | {change} | {from which perspectives} | {what could go wrong} |
...
```

### 5.3 Industry Alignment Checklist

Verify the proposed changes align with industry standards:

```markdown
### Industry Alignment

| Standard | Current State | After Refactoring |
|----------|---------------|-------------------|
| Single Responsibility | {status} | {expected} |
| Dependency Injection | {status} | {expected} |
| Error Handling Pattern | {status} | {expected} |
| Naming Conventions | {status} | {expected} |
| API Consistency | {status} | {expected} |
```

### 5.4 Testability Assessment

```markdown
### Testability

| Aspect | Before | After |
|--------|--------|-------|
| Can test in isolation? | {yes/no} | {yes/no} |
| Dependencies mockable? | {yes/no} | {yes/no} |
| State deterministic? | {yes/no} | {yes/no} |
| Error paths testable? | {yes/no} | {yes/no} |
```

### 5.5 Performance Considerations

```markdown
### Performance

| Concern | Current | After Refactoring |
|---------|---------|-------------------|
| Allocations per operation | {estimate} | {expected} |
| Abstraction layers in hot path | {count} | {expected} |
| Caching opportunities | {utilized?} | {expected} |
```

---

## Phase 6: Output

**IMPORTANT:** Output is a REPORT only. Do NOT modify any code files. Do NOT implement any recommendations. Return the analysis to the user for their decision.

### 6.1 Executive Summary

One paragraph capturing:
- What the component does
- The core problem identified
- The recommended solution
- Expected improvements

### 6.2 Detailed Report

Full report following this structure:

```markdown
# Recombination Report: {Component Name}

## Summary
{executive summary}

## Understanding
{from Phase 1}

## Perspectives Generated
{all 5 from Phase 2}

## Selected Perspectives
{from Phase 3}

## Cross-Critique
{from Phase 4}

## Synthesis
{from Phase 5}

## Implementation Recommendations
{ordered list of specific code changes}

## Metrics
- Estimated complexity reduction: {%}
- Estimated test coverage improvement: {%}
- Industry alignment score: {1-5}
```

---

## Reference: Industry Standards Checklist

Use this checklist when evaluating against industry standards:

### API Design
- [ ] Consistent naming (camelCase or snake_case, not mixed)
- [ ] Standard HTTP/gRPC methods where applicable
- [ ] Error types follow established patterns (structured errors with kind/code)
- [ ] Options/config use builder or options pattern consistently
- [ ] Timeouts and cancellation supported where operations can block

### Component Design (SOLID)
- [ ] Single Responsibility: component does one thing well
- [ ] Open/Closed: extensible without modification
- [ ] Liskov Substitution: subtypes are substitutable
- [ ] Interface Segregation: no forced dependency on unused methods
- [ ] Dependency Inversion: depends on abstractions, not concretions

### Code Smells to Check
- [ ] No duplicated code (DRY)
- [ ] No long parameter lists (use options struct)
- [ ] No primitive obsession (use proper types)
- [ ] No feature envy (method uses another class more than its own)
- [ ] No inappropriate intimacy (classes too coupled)
- [ ] No refused bequest (inheriting unwanted methods)
- [ ] No speculative generality (over-abstraction for future needs)

### Performance Patterns
- [ ] Avoid allocations in hot paths
- [ ] Pool reusable resources
- [ ] Lazy initialization where appropriate
- [ ] Batch operations when possible
- [ ] Consider caching for expensive computations

### Testability Patterns
- [ ] Dependencies injected, not created internally
- [ ] No global state (or clearly isolated)
- [ ] Pure functions where possible
- [ ] Deterministic behavior (no time/random dependencies in core logic)
- [ ] Error paths explicit and testable

---

## Anti-Patterns

**Do not:**
- Recommend changes without code references
- Generate vague perspectives ("could be better")
- Skip the cross-critique phase
- Propose changes that don't address identified issues
- Ignore performance context for runtime code
- Add abstraction layers without justification
- Recommend industry patterns that don't fit the context

---

## Example Invocation

```
Review component: runtime/lua/modules/websocket/
Context: runtime (performance matters)
Performance priority: critical

Follow recombination protocol: protocols/recombination.md
```

---

## Sources

This protocol synthesizes practices from:
- [Clean Architecture principles](https://blog.ndepend.com/clean-architecture-refactoring-a-case-study/)
- [SOLID Design Principles](https://www.digitalocean.com/community/conceptual-articles/s-o-l-i-d-the-first-five-principles-of-object-oriented-design)
- [Dialectical synthesis for LLM reasoning](https://arxiv.org/html/2501.14917v3)
- [Code Smells catalog](https://refactoring.guru/refactoring/smells)
- [Google API Design Guide](https://cloud.google.com/apis/design)
- [Dependency Inversion Principle](https://stackify.com/dependency-inversion-principle/)
