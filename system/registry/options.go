package registry

import "github.com/wippyai/runtime/api/registry"

// Option configures registry behavior at construction time.
type Option func(*Reg)

// WithKindDirective registers a directive for a specific entry kind.
func WithKindDirective(kind registry.Kind, exp registry.Directive) Option {
	return func(r *Reg) {
		if exp == nil || kind == "" {
			return
		}
		if r.directivesByKind == nil {
			r.directivesByKind = make(map[registry.Kind][]registry.Directive)
		}
		r.directivesByKind[kind] = append(r.directivesByKind[kind], exp)
	}
}
