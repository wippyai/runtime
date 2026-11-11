// SPDX-License-Identifier: MPL-2.0

package xml

import (
	"strings"
	"unicode/utf8"
)

func (w *Encoder) binding(prefix string) string {
	uri, _ := w.ns.lookup(prefix)
	return uri
}

// declare binds prefix on the element whose declarations start at scope, replacing a
// binding of the same prefix already declared there.
func (w *Encoder) declare(prefix, uri string, scope int) error {
	if i, ok := w.ns.visibleAt(prefix); ok && i >= scope {
		w.ns.rebind(i, uri)
		return nil
	}
	if w.ns.len() >= w.opts.MaxNamespaces {
		return w.limit("max_namespaces")
	}
	w.ns.push(Binding{prefix, uri})
	return nil
}

// declareRoot declares the preferred bindings on the root element.
func (w *Encoder) declareRoot(scope int) error {
	if w.root {
		return nil
	}
	for p, u := range w.opts.NS {
		if p != "xml" {
			if err := w.declare(p, u, scope); err != nil {
				return err
			}
		}
	}
	return nil
}

func (w *Encoder) prefix(uri string, attribute bool, scope int) (string, error) {
	if uri == XMLNamespace {
		return "xml", nil
	}
	if !attribute && w.binding("") == uri {
		return "", nil
	}
	if best := w.ns.smallestPrefix(uri); best != "" {
		return best, nil
	}
	if !attribute {
		return "", w.declare("", uri, scope)
	}
	for {
		w.generated++
		p := "ns" + decimal(w.generated)
		if _, taken := w.ns.visibleAt(p); !taken {
			return p, w.declare(p, uri, scope)
		}
	}
}

func decimal(n int) string {
	var a [20]byte
	i := len(a)
	for {
		i--
		a[i] = byte(n%10) + '0'
		n /= 10
		if n == 0 {
			break
		}
	}
	return string(a[i:])
}

func (w *Encoder) name(prefix, local string) {
	if prefix != "" {
		w.buf = append(w.buf, prefix...)
		w.buf = append(w.buf, ':')
	}
	w.buf = append(w.buf, local...)
}

// knownName reports whether s is a valid XML name. Valid names up to the decoder's
// interning cutoff are remembered as copies; longer names are validated every time.
func (w *Encoder) knownName(s string) bool {
	if s == "" {
		return false
	}
	if len(s) > maxInternBytes {
		return validName(s)
	}
	slot := &w.valid[(len(s)*31+int(s[0])*7+int(s[len(s)-1]))&(recentNames-1)]
	if *slot == s {
		return true
	}
	if !validName(s) {
		return false
	}
	*slot = strings.Clone(s)
	return true
}

func (w *Encoder) expanded(key string) (string, string, error) {
	local := key[strings.LastIndexByte(key, '}')+1:]
	if int64(len(local)) >= w.maxTag {
		return "", "", w.limit("max_tag_bytes")
	}
	if !strings.HasPrefix(key, "{") {
		if !w.knownName(key) || key == "xmlns" {
			return "", "", problem("element", "invalid attribute name")
		}
		return "", key, nil
	}
	i := strings.LastIndexByte(key, '}')
	if i < 2 || !w.knownName(key[i+1:]) {
		return "", "", problem("element", "invalid expanded attribute name")
	}
	ns := key[1:i]
	if ns == xmlnsNamespace {
		return "", "", problem("namespace", "reserved attribute namespace")
	}
	return ns, key[i+1:], nil
}

func validateOutputBinding(prefix, uri string) error {
	if err := validateBinding(prefix, uri); err != nil {
		return err
	}
	if !utf8.ValidString(uri) {
		return problem("namespace", "malformed namespace URI")
	}
	for _, r := range uri {
		if !validChar(r) {
			return problem("namespace", "invalid namespace URI character")
		}
	}
	return nil
}
