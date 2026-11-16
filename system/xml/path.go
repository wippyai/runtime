// SPDX-License-Identifier: MPL-2.0

package xml

import (
	"errors"
	"io"
	"strings"
)

type step struct {
	name    string
	ns      string
	anyName bool
	anyNS   bool
}

// Path holds compiled expanded-name selection steps.
type Path struct {
	steps []step
}

// CompilePath compiles a path using the supplied namespace bindings.
func CompilePath(path string, bindings map[string]string) (Path, error) {
	var p Path
	path = strings.TrimPrefix(path, "/")
	for _, part := range strings.Split(path, "/") {
		s := step{}
		switch {
		case part == "*":
			s.anyName = true
			s.anyNS = true
		case strings.HasPrefix(part, "*:"):
			s.anyNS = true
			s.name = strings.Clone(part[2:])
			if !validName(s.name) {
				return p, problem("argument", "invalid path name")
			}
		default:
			prefix, local, qualified := strings.Cut(part, ":")
			if !qualified {
				local = prefix
				prefix = ""
			}
			if local == "*" && qualified {
				s.anyName = true
			} else if !validName(local) {
				return p, problem("argument", "invalid path name")
			}
			s.name = strings.Clone(local)
			if qualified {
				if !validName(prefix) {
					return p, problem("argument", "invalid path prefix")
				}
				var ok bool
				s.ns, ok = bindings[prefix]
				if prefix == "xml" {
					s.ns = XMLNamespace
					ok = true
				}
				if !ok {
					return p, problem("namespace", "unbound path prefix")
				}
			}
		}
		p.steps = append(p.steps, s)
	}
	return p, nil
}

func (s step) matches(name, ns string) bool {
	return (s.anyName || s.name == name) && (s.anyNS || s.ns == ns)
}

// Len returns the number of steps.
func (p Path) Len() int { return len(p.steps) }

// Match reports whether step i matches an element with the expanded name.
func (p Path) Match(i int, name, ns string) bool {
	return i >= 0 && i < len(p.steps) && p.steps[i].matches(name, ns)
}

// Find returns descendants matching a relative path.
func Find(e *Element, p Path) []*Element {
	var result []*Element
	var walk func(*Element, int)
	walk = func(e *Element, i int) {
		if e == nil || i >= len(p.steps) {
			return
		}
		for _, v := range e.Children {
			if child, ok := v.(*Element); ok && child != nil && p.steps[i].matches(child.Name, child.NS) {
				if i == len(p.steps)-1 {
					result = append(result, child)
				} else {
					walk(child, i+1)
				}
			}
		}
	}
	walk(e, 0)
	return result
}

// Selector streams outermost matches in document order.
type Selector struct {
	err     error
	decoder *Decoder
	paths   []Path
	done    bool
}

// NewSelector transfers cursor ownership to a path selector.
func NewSelector(d *Decoder, paths []Path) (*Selector, error) {
	if d == nil || len(paths) == 0 {
		return nil, problem("argument", "decoder and paths are required")
	}
	for _, p := range paths {
		if len(p.steps) == 0 {
			return nil, problem("argument", "empty path")
		}
	}
	if d.selected || d.initialized {
		return nil, problem("state", "selection requires a fresh decoder")
	}
	d.selected = true
	return &Selector{decoder: d, paths: append([]Path(nil), paths...)}, nil
}

func (s *Selector) matches() bool {
	for _, p := range s.paths {
		if len(p.steps) != len(s.decoder.stack) {
			continue
		}
		match := true
		for i, f := range s.decoder.stack {
			if !p.steps[i].matches(f.name, f.ns) {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// Read appends records in document order, stopping after n or the source span budget.
func (s *Selector) Read(b *Batch, n int) (int, error) {
	s.decoder.selectionReading = true
	defer func() { s.decoder.selectionReading = false }()
	if s.err != nil {
		return 0, s.err
	}
	if s.done {
		return 0, io.EOF
	}
	if b == nil || n < 0 {
		return 0, problem("argument", "invalid record batch")
	}
	if n == 0 {
		n = 64
	}
	count := 0
	carried := 0
	var start int64
	for count < n {
		ev, err := s.decoder.Next()
		if errors.Is(err, io.EOF) {
			s.done = true
			if count > 0 {
				return count, nil
			}
			return 0, io.EOF
		}
		if err != nil {
			s.err = err
			return count, err
		}
		if ev.Kind == StartEvent && s.matches() {
			if count == 0 {
				start = ev.Pos.Offset
			}
			scope, err := s.decoder.visitNode(b.appendEvent)
			if err != nil {
				s.err = err
				return count, err
			}
			count++
			carried += scope
			if s.decoder.current.EndOffset-start+int64(carried) >= int64(s.decoder.opts.MaxNodeBytes) {
				break
			}
		}
	}
	return count, nil
}

// Err returns the failure that made the selection terminal, or nil.
func (s *Selector) Err() error { return s.err }

// Close closes the owned decoder.
func (s *Selector) Close() {
	s.decoder.Close()
	s.done = true
}
