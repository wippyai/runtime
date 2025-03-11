package metamatch

import (
	"github.com/ponyruntime/pony/api/registry"
)

// MatchAny creates a matcher that returns true if any of the provided matchers returns true
func MatchAny(matchers ...*Matcher) *Matcher {
	return NewMatcher().AddCondition(&anyMatcherCondition{matchers: matchers})
}

// MatchAll creates a matcher that returns true if all of the provided matchers return true
// This is functionally equivalent to chaining conditions on a single matcher
func MatchAll(matchers ...*Matcher) *Matcher {
	return NewMatcher().AddCondition(&allMatcherCondition{matchers: matchers})
}

// MatchNone creates a matcher that returns true if none of the provided matchers returns true
func MatchNone(matchers ...*Matcher) *Matcher {
	return NewMatcher().AddCondition(&noneMatcherCondition{matchers: matchers})
}

// Not creates a matcher that negates the result of the provided matcher
func Not(matcher *Matcher) *Matcher {
	return NewMatcher().AddCondition(&notMatcherCondition{matcher: matcher})
}

// Filter filters a slice of metadata entries using the provided matcher
func Filter(entries []registry.Metadata, matcher *Matcher) []registry.Metadata {
	if matcher == nil {
		return entries
	}

	result := make([]registry.Metadata, 0, len(entries))
	for _, entry := range entries {
		if matcher.Match(entry) {
			result = append(result, entry)
		}
	}
	return result
}

// Composite condition implementations

type anyMatcherCondition struct {
	matchers []*Matcher
}

func (c *anyMatcherCondition) Match(metadata registry.Metadata) bool {
	if len(c.matchers) == 0 {
		return false
	}

	for _, matcher := range c.matchers {
		if matcher.Match(metadata) {
			return true
		}
	}
	return false
}

type allMatcherCondition struct {
	matchers []*Matcher
}

func (c *allMatcherCondition) Match(metadata registry.Metadata) bool {
	if len(c.matchers) == 0 {
		return true
	}

	for _, matcher := range c.matchers {
		if !matcher.Match(metadata) {
			return false
		}
	}
	return true
}

type noneMatcherCondition struct {
	matchers []*Matcher
}

func (c *noneMatcherCondition) Match(metadata registry.Metadata) bool {
	for _, matcher := range c.matchers {
		if matcher.Match(metadata) {
			return false
		}
	}
	return true
}

type notMatcherCondition struct {
	matcher *Matcher
}

func (c *notMatcherCondition) Match(metadata registry.Metadata) bool {
	return !c.matcher.Match(metadata)
}
