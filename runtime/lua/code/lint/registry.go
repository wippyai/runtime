package lint

import (
	"sort"
	"sync"
)

// Registry manages lint rules and their configuration
type Registry struct {
	rules    map[string]Rule
	severity map[string]Severity
	order    []string
	mu       sync.RWMutex
}

// NewRegistry creates an empty rule registry
func NewRegistry() *Registry {
	return &Registry{
		rules:    make(map[string]Rule),
		severity: make(map[string]Severity),
	}
}

// Register adds a rule to the registry
func (r *Registry) Register(rule Rule) {
	if rule == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	name := rule.Meta().Name
	if _, exists := r.rules[name]; !exists {
		r.order = append(r.order, name)
	}
	r.rules[name] = rule
}

// SetSeverity overrides the severity for a rule
func (r *Registry) SetSeverity(name string, severity Severity) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.severity[name] = severity
}

// GetSeverity returns the effective severity for a rule
func (r *Registry) GetSeverity(name string) Severity {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.getSeverityLocked(name)
}

// getSeverityLocked returns severity without locking (caller must hold lock)
func (r *Registry) getSeverityLocked(name string) Severity {
	if sev, ok := r.severity[name]; ok {
		return sev
	}
	if rule, ok := r.rules[name]; ok {
		return rule.Meta().DefaultSeverity
	}
	return SeverityOff
}

// IsEnabled returns true if the rule is not disabled
func (r *Registry) IsEnabled(name string) bool {
	return r.GetSeverity(name) != SeverityOff
}

// Rules returns all registered rules in order
func (r *Registry) Rules() []Rule {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]Rule, 0, len(r.order))
	for _, name := range r.order {
		if rule, ok := r.rules[name]; ok {
			result = append(result, rule)
		}
	}
	return result
}

// EnabledRules returns only rules that are not disabled
func (r *Registry) EnabledRules() []Rule {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]Rule, 0, len(r.order))
	for _, name := range r.order {
		if r.getSeverityLocked(name) == SeverityOff {
			continue
		}
		if rule, ok := r.rules[name]; ok {
			result = append(result, rule)
		}
	}
	return result
}

// RulesByCategory returns rules grouped by category
func (r *Registry) RulesByCategory() map[string][]Rule {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make(map[string][]Rule)
	for _, name := range r.order {
		if rule, ok := r.rules[name]; ok {
			cat := rule.Meta().Category
			result[cat] = append(result[cat], rule)
		}
	}
	return result
}

// Categories returns all unique categories sorted
func (r *Registry) Categories() []string {
	byCategory := r.RulesByCategory()
	cats := make([]string, 0, len(byCategory))
	for cat := range byCategory {
		cats = append(cats, cat)
	}
	sort.Strings(cats)
	return cats
}

// Clone creates a copy with independent severity configuration
func (r *Registry) Clone() *Registry {
	r.mu.RLock()
	defer r.mu.RUnlock()

	clone := &Registry{
		rules:    r.rules, // rules are immutable, share reference
		severity: make(map[string]Severity, len(r.severity)),
		order:    make([]string, len(r.order)),
	}
	for k, v := range r.severity {
		clone.severity[k] = v
	}
	copy(clone.order, r.order)
	return clone
}

// DefaultRegistry is the global rule registry
var DefaultRegistry = NewRegistry()

// Register adds a rule to the default registry
func Register(rule Rule) {
	DefaultRegistry.Register(rule)
}
