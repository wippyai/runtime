# Metamatch Package Specification

## Overview

Metamatch is a flexible metadata matching library for Go, designed to filter and match registry metadata maps in a
declarative, composable way. It provides a fluent API for building matchers that can evaluate whether a given metadata
set satisfies specific criteria.

## Core Components

### Matcher

The `Matcher` is the main entry point for the package. It contains a collection of conditions, all of which must be
satisfied for a match to occur.

```go
// Create a new empty matcher
matcher := metamatch.NewMatcher()
```

A matcher without conditions will match everything. As you add conditions, the matcher becomes more specific.

### Conditions

Conditions are the building blocks of matchers. Each condition checks a specific aspect of the metadata. Conditions are
added to matchers using the fluent API methods.

## Basic Matching Methods

### WithStringValue

Matches if the key exists with the exact string value:

```go
matcher.WithStringValue("name", "backup-service")
```

### WithBoolValue

Matches if the key exists with the specified boolean value:

```go
matcher.WithBoolValue("enabled", true)
```

### WithIntValue

Matches if the key exists with the specified integer value:

```go
matcher.WithIntValue("retries", 3)
```

### WithExactValue

Matches if the key exists with the exact value (using Go's equality comparison):

```go
matcher.WithExactValue("config", myConfigObject)
```

## Pattern Matching Methods

### WithStringPrefix

Matches if the key exists with a string value that has the specified prefix:

```go
matcher.WithStringPrefix("name", "backup-")
```

### WithRegexMatch

Matches if the key has a string value that matches the specified regex pattern:

```go
matcher.WithRegexMatch("version", `^\d+\.\d+\.\d+$`)
```

### WithKeyExists

Matches if the key exists (with any value):

```go
matcher.WithKeyExists("description")
```

### WithTagContains

Matches if the key exists as a tag array and contains the specified value:

```go
matcher.WithTagContains("tags", "critical")
```

## Logical Combinations

The package provides functions to create logical combinations of matchers:

### MatchAny (OR)

Matches if any of the provided matchers match:

```go
combinedMatcher := metamatch.MatchAny(
metamatch.NewMatcher().WithStringValue("type", "backup"),
metamatch.NewMatcher().WithStringValue("type", "restore")
)
```

### MatchAll (AND)

Matches if all of the provided matchers match:

```go
combinedMatcher := metamatch.MatchAll(
metamatch.NewMatcher().WithStringValue("env", "production"),
metamatch.NewMatcher().WithBoolValue("critical", true)
)
```

### MatchNone (NOR)

Matches if none of the provided matchers match:

```go
combinedMatcher := metamatch.MatchNone(
metamatch.NewMatcher().WithStringValue("status", "deprecated"),
metamatch.NewMatcher().WithStringValue("status", "experimental")
)
```

### Not (NOT)

Inverts the result of the provided matcher:

```go
invertedMatcher := metamatch.Not(
metamatch.NewMatcher().WithStringValue("status", "disabled")
)
```

## Filtering Collections

The package provides a utility function to filter collections of metadata:

```go
// Filter a slice of metadata entries
filteredEntries := metamatch.Filter(entries, matcher)
```

## Chaining Methods

All matcher methods return the matcher itself, allowing for method chaining:

```go
matcher := metamatch.NewMatcher().
WithStringValue("name", "backup-service").
WithTagContains("tags", "critical").
WithBoolValue("enabled", true)
```

## Advanced Usage Examples

### Complex Service Discovery

```go
// Find production services supporting a specific API version
matcher := metamatch.NewMatcher().
WithStringValue("env", "production").
WithRegexMatch("api_version", `^2\.\d+\.\d+$`).
WithTagContains("capabilities", "data-processing")
```

### Feature Flag Checking

```go
// Check if a feature is enabled for a specific component and environment
isEnabled := metamatch.MatchAll(
metamatch.NewMatcher().WithStringValue("component", "billing"),
metamatch.NewMatcher().WithStringValue("env", "staging"),
metamatch.NewMatcher().WithBoolValue("feature.new-pricing", true)
).Match(metadata)
```

### Resource Selection

```go
// Select resources matching complex criteria
resources := metamatch.Filter(allResources,
metamatch.MatchAny(
metamatch.NewMatcher().
WithStringPrefix("name", "db-").
WithTagContains("type", "mysql"),
metamatch.NewMatcher().
WithStringPrefix("name", "cache-").
WithTagContains("type", "redis").
WithIntValue("priority", 1)
)
)
```

## Best Practices

1. Start with a broad matcher and then add more specific conditions.
2. For complex logic, break down into smaller matchers and combine them.
3. Use `MatchAny`, `MatchAll`, and `Not` to create sophisticated matching logic.
4. When filtering large collections, put the most restrictive conditions first.
5. For regular expressions, ensure your patterns are valid and efficient.

## Implementation Details

The package implements the Condition interface that all specific matchers must fulfill:

```go
type Condition interface {
// Match checks if the metadata meets this condition
Match(metadata registry.Metadata) bool
}
```

Internal condition types handle specific matching strategies, abstracting the implementation details from users of the
API.