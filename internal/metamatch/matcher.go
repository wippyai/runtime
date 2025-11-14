package metamatch

import (
	"regexp"
	"strings"

	"github.com/wippyai/runtime/api/registry"
)

// Condition represents a matcher for a single metadata field
type Condition interface {
	// Match checks if the metadata meets this condition
	Match(metadata registry.Metadata) bool
}

// Matcher defines a composite pattern for matching metadata entries
// It can contain multiple conditions that all need to be satisfied
type Matcher struct {
	conditions []Condition
}

// NewMatcher creates a new empty metadata matcher
func NewMatcher() *Matcher {
	return &Matcher{
		conditions: []Condition{},
	}
}

// Match checks if the metadata satisfies all conditions in this matcher
func (m *Matcher) Match(metadata registry.Metadata) bool {
	if len(m.conditions) == 0 {
		return true // Empty matcher matches everything
	}

	for _, cond := range m.conditions {
		if !cond.Match(metadata) {
			return false
		}
	}
	return true
}

// AddCondition adds a new condition to this matcher
func (m *Matcher) AddCondition(cond Condition) *Matcher {
	m.conditions = append(m.conditions, cond)
	return m
}

// WithExactValue adds a condition that matches if the key exists with the exact value
func (m *Matcher) WithExactValue(key string, value interface{}) *Matcher {
	return m.AddCondition(&exactValueCondition{key: key, value: value})
}

// WithStringValue adds a condition that matches if the key exists with the exact string value
func (m *Matcher) WithStringValue(key string, value string) *Matcher {
	return m.AddCondition(&stringValueCondition{key: key, value: value})
}

// WithStringPrefix adds a condition that matches if the key exists with a string value
// that has the specified prefix
func (m *Matcher) WithStringPrefix(key string, prefix string) *Matcher {
	return m.AddCondition(&stringPrefixCondition{key: key, prefix: prefix})
}

// WithTagContains adds a condition that matches if the key exists as a tag and
// contains the specified value
func (m *Matcher) WithTagContains(key string, value string) *Matcher {
	return m.AddCondition(&tagContainsCondition{key: key, value: value})
}

// WithKeyExists adds a condition that matches if the key exists (any value)
func (m *Matcher) WithKeyExists(key string) *Matcher {
	return m.AddCondition(&keyExistsCondition{key: key})
}

// WithRegexMatch adds a condition that matches if the key has a string value
// that matches the specified regex
func (m *Matcher) WithRegexMatch(key string, pattern string) *Matcher {
	re, err := regexp.Compile(pattern)
	if err != nil {
		// If the pattern is invalid, this condition will never match
		return m.AddCondition(&alwaysFalseCondition{reason: err.Error()})
	}
	return m.AddCondition(&regexMatchCondition{key: key, pattern: re})
}

// WithBoolValue adds a condition that matches if the key exists with the specified boolean value
func (m *Matcher) WithBoolValue(key string, value bool) *Matcher {
	return m.AddCondition(&boolValueCondition{key: key, value: value})
}

// WithIntValue adds a condition that matches if the key exists with the specified integer value
func (m *Matcher) WithIntValue(key string, value int) *Matcher {
	return m.AddCondition(&intValueCondition{key: key, value: value})
}

// Individual condition implementations

type exactValueCondition struct {
	key   string
	value interface{}
}

func (c *exactValueCondition) Match(metadata registry.Metadata) bool {
	val, exists := metadata[c.key]
	return exists && val == c.value
}

type stringValueCondition struct {
	key   string
	value string
}

func (c *stringValueCondition) Match(metadata registry.Metadata) bool {
	return metadata.StringValue(c.key) == c.value
}

type stringPrefixCondition struct {
	key    string
	prefix string
}

func (c *stringPrefixCondition) Match(metadata registry.Metadata) bool {
	val := metadata.StringValue(c.key)
	return strings.HasPrefix(val, c.prefix)
}

type keyExistsCondition struct {
	key string
}

func (c *keyExistsCondition) Match(metadata registry.Metadata) bool {
	_, exists := metadata[c.key]
	return exists
}

type tagContainsCondition struct {
	key   string
	value string
}

func (c *tagContainsCondition) Match(metadata registry.Metadata) bool {
	tags := metadata.TagValue(c.key)
	if tags == nil {
		return false
	}

	for _, tag := range tags {
		if tag == c.value {
			return true
		}
	}
	return false
}

type regexMatchCondition struct {
	key     string
	pattern *regexp.Regexp
}

func (c *regexMatchCondition) Match(metadata registry.Metadata) bool {
	val := metadata.StringValue(c.key)
	return c.pattern.MatchString(val)
}

type boolValueCondition struct {
	key   string
	value bool
}

func (c *boolValueCondition) Match(metadata registry.Metadata) bool {
	return metadata.BoolValue(c.key) == c.value
}

type intValueCondition struct {
	key   string
	value int
}

func (c *intValueCondition) Match(metadata registry.Metadata) bool {
	return metadata.IntValue(c.key) == c.value
}

type alwaysFalseCondition struct {
	reason string
}

func (c *alwaysFalseCondition) Match(_ registry.Metadata) bool {
	return false
}
