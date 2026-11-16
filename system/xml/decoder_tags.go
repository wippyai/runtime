// SPDX-License-Identifier: MPL-2.0

package xml

import "bytes"

func (d *Decoder) intern(p []byte) string {
	if s, ok := d.interned[string(p)]; ok {
		return s
	}
	s := string(p)
	if len(d.interned) < maxInternEntries && len(s) <= maxInternBytes {
		d.interned[s] = s
	}
	return s
}

func (d *Decoder) lookup(prefix string) (string, bool) { return d.ns.lookup(prefix) }

// Scope returns the effective namespace bindings at the current element.
func (d *Decoder) Scope() []Binding {
	return d.ns.visible(make([]Binding, 0, d.ns.len()))
}

func (d *Decoder) start(pos Position) error {
	if len(d.stack) == 0 && d.root {
		return d.fail("syntax", "multiple root elements", "")
	}
	if len(d.stack) >= d.opts.MaxDepth {
		return d.limit("max_depth")
	}
	quote := byte(0)
	n := 1
	for {
		if n >= d.opts.MaxTagBytes {
			return d.limit("max_tag_bytes")
		}
		if n >= d.hi-d.lo && !d.ensure(n+1) {
			if d.err != nil {
				return d.err
			}
			return d.fail("syntax", "unterminated start tag", "")
		}
		end := min(d.hi-d.lo, d.opts.MaxTagBytes)
		seg := d.buf[d.lo+n : d.lo+end]
		k := -1
		if quote != 0 {
			k = bytes.IndexByte(seg, quote)
		} else {
			for i, b := range seg {
				if b == '>' || b == '"' || b == '\'' {
					k = i
					break
				}
			}
		}
		if k < 0 {
			n = end
			continue
		}
		b := seg[k]
		n += k + 1
		if quote != 0 {
			quote = 0
		} else if b == '>' {
			break
		} else {
			quote = b
		}
	}
	p := d.buf[d.lo+1 : d.lo+n-1]
	empty := false
	if len(p) > 0 && p[len(p)-1] == '/' {
		p = p[:len(p)-1]
		empty = true
	}
	i := 0
	for i < len(p) && !space(p[i]) {
		i++
	}
	parsed, err := d.parseName(p[:i])
	raw, prefix, name := parsed.raw, parsed.prefix, parsed.local
	if err != nil {
		return d.wrap(err)
	}
	if prefix == "xmlns" {
		return d.fail("namespace", "reserved element prefix", "")
	}
	scope := d.ns.len()
	attrCount := 0
	// Reserving the tag size keeps attribute slices stable while normalization appends.
	if cap(d.scratch) < len(p) {
		d.scratch = make([]byte, 0, len(p))
	}
	for i < len(p) {
		if !space(p[i]) {
			return d.fail("syntax", "missing attribute whitespace", "")
		}
		for i < len(p) && space(p[i]) {
			i++
		}
		if i == len(p) {
			break
		}
		j := i
		for i < len(p) && !space(p[i]) && p[i] != '=' {
			i++
		}
		parsed, err := d.parseName(p[j:i])
		an, ap, al := parsed.raw, parsed.prefix, parsed.local
		if err != nil {
			return d.wrap(err)
		}
		for i < len(p) && space(p[i]) {
			i++
		}
		if i == len(p) || p[i] != '=' {
			return d.fail("syntax", "missing attribute equals", "")
		}
		i++
		for i < len(p) && space(p[i]) {
			i++
		}
		if i == len(p) || p[i] != '\'' && p[i] != '"' {
			return d.fail("syntax", "unquoted attribute", "")
		}
		q := p[i]
		i++
		j = i
		for i < len(p) && p[i] != q {
			i++
		}
		if i == len(p) {
			return d.fail("syntax", "unterminated attribute", "")
		}
		v, err := d.normalize(p[j:i], true, true, 1+j)
		if err != nil {
			return err
		}
		i++
		attrCount++
		if attrCount > d.opts.MaxAttrs {
			return d.limit("max_attrs")
		}
		if an == "xmlns" || ap == "xmlns" {
			bp := al
			if an == "xmlns" {
				bp = ""
			}
			if d.ns.declared(bp, scope) {
				return d.fail("syntax", "duplicate namespace declaration", "")
			}
			uri := d.intern(v)
			if err = validateBinding(bp, uri); err != nil {
				return d.wrap(err)
			}
			if d.ns.len() >= d.opts.MaxNamespaces {
				return d.limit("max_namespaces")
			}
			d.ns.push(Binding{bp, uri})
			if bp != "xml" {
				d.declarations = append(d.declarations, Binding{bp, uri})
			}
		} else {
			for _, a := range d.attributes {
				if a.Name == al && a.NS == ap {
					return d.fail("syntax", "duplicate attribute", "")
				}
			}
			d.attributes = append(d.attributes, Attribute{Name: al, NS: ap, Value: v})
		}
	}
	ns, ok := d.lookup(prefix)
	if !ok {
		return d.fail("namespace", "unbound element prefix", "")
	}
	preserve := false
	if len(d.stack) > 0 {
		preserve = d.stack[len(d.stack)-1].preserve
	}
	for i := range d.attributes {
		a := &d.attributes[i]
		if a.NS != "" {
			u, ok := d.lookup(a.NS)
			if !ok {
				return d.fail("namespace", "unbound attribute prefix", "")
			}
			a.NS = u
		}
		for _, earlier := range d.attributes[:i] {
			if earlier.Name == a.Name && earlier.NS == a.NS {
				return d.fail("namespace", "duplicate expanded attribute", "")
			}
		}
		if a.NS == XMLNamespace && a.Name == "space" {
			switch string(a.Value) {
			case "preserve":
				preserve = true
			case "default":
				preserve = false
			}
		}
	}
	if err = d.consume(n); err != nil {
		return err
	}
	d.root = true
	d.empty = empty
	d.stack = append(d.stack, frame{qname: raw, name: name, ns: ns, scope: scope, preserve: preserve})
	d.current = Event{Kind: StartEvent, Name: name, NS: ns, Attrs: d.attributes, XMLNS: d.declarations, Pos: pos, Depth: len(d.stack), EndOffset: d.pos.Offset}
	return nil
}

func validateBinding(prefix, uri string) error {
	if prefix != "" && !validName(prefix) || prefix == "xmlns" || uri == xmlnsNamespace || prefix == "xml" && uri != XMLNamespace || prefix != "xml" && uri == XMLNamespace || prefix != "" && uri == "" {
		return problem("namespace", "invalid namespace binding")
	}
	return nil
}

// recentSlot picks a slot of the recent-name cache from cheap features of the name.
func recentSlot(p []byte) int {
	return (len(p)*31 + int(p[0])*7 + int(p[len(p)-1])) & (recentNames - 1)
}

func (d *Decoder) parseName(p []byte) (parsedName, error) {
	if len(p) == 0 {
		return parsedName{}, problem("syntax", "invalid XML name")
	}
	slot := &d.recent[recentSlot(p)]
	if slot.raw == string(p) && slot.raw != "" {
		return *slot, nil
	}
	if name, ok := d.names[string(p)]; ok {
		*slot = name
		return name, nil
	}
	raw := string(p)
	prefix, local, err := qname(raw)
	if err != nil {
		return parsedName{}, err
	}
	name := parsedName{raw: raw, prefix: prefix, local: local}
	if len(raw) <= maxInternBytes {
		if len(d.names) >= maxInternEntries {
			clear(d.names)
		}
		d.names[raw] = name
		*slot = name
	}
	return name, nil
}
