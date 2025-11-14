package metamatch

import (
	"testing"

	"github.com/wippyai/runtime/api/registry"
)

func TestBasicMatchers(t *testing.T) {
	sampleMetadata := registry.Metadata{
		"name":    "test-operation",
		"version": "1.0.0",
		"enabled": true,
		"count":   42,
		"tags":    []string{"critical", "maintenance"},
	}

	tests := []struct {
		name     string
		matcher  *Matcher
		expected bool
	}{
		{
			name:     "Empty matcher should match everything",
			matcher:  NewMatcher(),
			expected: true,
		},
		{
			name:     "Exact string value match",
			matcher:  NewMatcher().WithStringValue("name", "test-operation"),
			expected: true,
		},
		{
			name:     "String value non-match",
			matcher:  NewMatcher().WithStringValue("name", "wrong-name"),
			expected: false,
		},
		{
			name:     "String prefix match",
			matcher:  NewMatcher().WithStringPrefix("name", "test-"),
			expected: true,
		},
		{
			name:     "String prefix non-match",
			matcher:  NewMatcher().WithStringPrefix("name", "wrong-"),
			expected: false,
		},
		{
			name:     "Key exists match",
			matcher:  NewMatcher().WithKeyExists("version"),
			expected: true,
		},
		{
			name:     "Key exists non-match",
			matcher:  NewMatcher().WithKeyExists("non-existent"),
			expected: false,
		},
		{
			name:     "Boolean value match",
			matcher:  NewMatcher().WithBoolValue("enabled", true),
			expected: true,
		},
		{
			name:     "Boolean value non-match",
			matcher:  NewMatcher().WithBoolValue("enabled", false),
			expected: false,
		},
		{
			name:     "Int value match",
			matcher:  NewMatcher().WithIntValue("count", 42),
			expected: true,
		},
		{
			name:     "Int value non-match",
			matcher:  NewMatcher().WithIntValue("count", 24),
			expected: false,
		},
		{
			name:     "Tag contains match",
			matcher:  NewMatcher().WithTagContains("tags", "critical"),
			expected: true,
		},
		{
			name:     "Tag contains non-match",
			matcher:  NewMatcher().WithTagContains("tags", "non-existent"),
			expected: false,
		},
		{
			name:     "Regex match",
			matcher:  NewMatcher().WithRegexMatch("version", `^\d+\.\d+\.\d+$`),
			expected: true,
		},
		{
			name:     "Regex non-match",
			matcher:  NewMatcher().WithRegexMatch("version", `^v\d+\.\d+\.\d+$`),
			expected: false,
		},
		{
			name:     "Multiple conditions - all match",
			matcher:  NewMatcher().WithStringValue("name", "test-operation").WithBoolValue("enabled", true),
			expected: true,
		},
		{
			name:     "Multiple conditions - some non-match",
			matcher:  NewMatcher().WithStringValue("name", "test-operation").WithIntValue("count", 100),
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.matcher.Match(sampleMetadata)
			if result != tt.expected {
				t.Errorf("Expected match result to be %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestCompositeMatchers(t *testing.T) {
	sampleMetadata := registry.Metadata{
		"name":     "test-operation",
		"version":  "1.0.0",
		"enabled":  true,
		"priority": "high",
		"tags":     []string{"critical", "maintenance"},
	}

	matcherA := NewMatcher().WithStringValue("name", "test-operation")
	matcherB := NewMatcher().WithBoolValue("enabled", true)
	matcherC := NewMatcher().WithStringValue("priority", "low")

	tests := []struct {
		name     string
		matcher  *Matcher
		expected bool
	}{
		{
			name:     "MatchAny - some match",
			matcher:  MatchAny(matcherA, matcherC),
			expected: true,
		},
		{
			name:     "MatchAny - none match",
			matcher:  MatchAny(NewMatcher().WithStringValue("name", "wrong"), matcherC),
			expected: false,
		},
		{
			name:     "MatchAll - all match",
			matcher:  MatchAll(matcherA, matcherB),
			expected: true,
		},
		{
			name:     "MatchAll - some non-match",
			matcher:  MatchAll(matcherA, matcherC),
			expected: false,
		},
		{
			name:     "MatchNone - none match",
			matcher:  MatchNone(NewMatcher().WithStringValue("name", "wrong"), matcherC),
			expected: true,
		},
		{
			name:     "MatchNone - some match",
			matcher:  MatchNone(matcherA, matcherC),
			expected: false,
		},
		{
			name:     "Not - invert true to false",
			matcher:  Not(matcherA),
			expected: false,
		},
		{
			name:     "Not - invert false to true",
			matcher:  Not(matcherC),
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.matcher.Match(sampleMetadata)
			if result != tt.expected {
				t.Errorf("Expected match result to be %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestFilter(t *testing.T) {
	metadataEntries := []registry.Metadata{
		{"name": "op1", "type": "maintenance", "priority": "high"},
		{"name": "op2", "type": "backup", "priority": "medium"},
		{"name": "op3", "type": "maintenance", "priority": "low"},
		{"name": "op4", "type": "migration", "priority": "high"},
	}

	tests := []struct {
		name          string
		matcher       *Matcher
		expectedLen   int
		expectedNames []string
	}{
		{
			name:          "Filter by type",
			matcher:       NewMatcher().WithStringValue("type", "maintenance"),
			expectedLen:   2,
			expectedNames: []string{"op1", "op3"},
		},
		{
			name:          "Filter by priority",
			matcher:       NewMatcher().WithStringValue("priority", "high"),
			expectedLen:   2,
			expectedNames: []string{"op1", "op4"},
		},
		{
			name:          "Filter by multiple criteria",
			matcher:       NewMatcher().WithStringValue("type", "maintenance").WithStringValue("priority", "high"),
			expectedLen:   1,
			expectedNames: []string{"op1"},
		},
		{
			name:          "No matches",
			matcher:       NewMatcher().WithStringValue("type", "non-existent"),
			expectedLen:   0,
			expectedNames: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := Filter(metadataEntries, tt.matcher)

			if len(result) != tt.expectedLen {
				t.Errorf("Expected %d results, got %d", tt.expectedLen, len(result))
			}

			// Check each expected name is in the result
			for _, expectedName := range tt.expectedNames {
				found := false
				for _, entry := range result {
					if entry.StringValue("name") == expectedName {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Expected to find entry with name '%s' in results", expectedName)
				}
			}
		})
	}
}
