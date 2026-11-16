// SPDX-License-Identifier: MPL-2.0

package xml

import (
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

// EncodeOptions configures namespaces, indentation and writer limits.
type EncodeOptions struct {
	// NS provides preferred bindings declared on the root.
	NS map[string]string
	// Indent contains the spaces or tabs added per level.
	Indent string
	// MaxDepth limits nesting during serialization.
	MaxDepth int
	// MaxAttrs limits attributes plus namespace declarations written on one element.
	MaxAttrs int
	// MaxNamespaces limits the declarations in scope at once, shadowed ones included.
	MaxNamespaces int
	// MaxNodeBytes limits the serialized size of one tree written between BeginTree and EndTree.
	MaxNodeBytes int
	// MaxTagBytes limits the serialized size of one start tag or comment.
	MaxTagBytes int
	// Declaration enables the UTF-8 XML declaration.
	Declaration bool
}

type outputFrame struct {
	name    string
	ns      string
	prefix  string
	scope   int
	tag     int64
	pending bool
	text    bool
}

// AttrValue is an attribute keyed as in Element.Attrs.
type AttrValue struct {
	// Key is the local name, or {uri}local for a qualified attribute.
	Key string
	// Value is the attribute value.
	Value string
}

type outputAttribute struct {
	name   string
	ns     string
	value  string
	prefix string
	data   []byte
}

// Encoder serializes XML into a drainable buffer.
type Encoder struct {
	err        error
	buf        []byte
	ns         namespaces
	order      []Binding
	stack      []outputFrame
	attributes []outputAttribute
	values     []AttrValue
	valid      [recentNames]string
	opts       EncodeOptions
	generated  int
	written    int64
	treeStart  int64
	maxTag     int64
	maxNode    int64
	root       bool
	finished   bool
}

// NewEncoder creates a writer with validated options.
func NewEncoder(opts EncodeOptions) (*Encoder, error) {
	if opts.MaxDepth < 0 || opts.MaxAttrs < 0 || opts.MaxNamespaces < 0 || opts.MaxNodeBytes < 0 || opts.MaxTagBytes < 0 || len(opts.Indent) > maxIndent || strings.Trim(opts.Indent, " \t") != "" {
		return nil, problem("argument", "invalid writer option")
	}
	if opts.MaxDepth == 0 {
		opts.MaxDepth = defaultMaxDepth
	}
	if opts.MaxAttrs == 0 {
		opts.MaxAttrs = defaultMaxAttrs
	}
	if opts.MaxNamespaces == 0 {
		opts.MaxNamespaces = defaultMaxNamespaces
	}
	if opts.MaxNodeBytes == 0 {
		opts.MaxNodeBytes = defaultMaxNodeBytes
	}
	if opts.MaxTagBytes == 0 {
		opts.MaxTagBytes = defaultMaxTokenBytes
	}
	for p, u := range opts.NS {
		if err := validateOutputBinding(p, u); err != nil {
			return nil, err
		}
	}
	ns := make(map[string]string, len(opts.NS))
	for p, u := range opts.NS {
		ns[p] = u
	}
	opts.NS = ns
	w := &Encoder{opts: opts, ns: indexed(), treeStart: -1, maxTag: int64(opts.MaxTagBytes), maxNode: int64(opts.MaxNodeBytes)}
	if opts.Declaration {
		w.buf = append(w.buf, `<?xml version="1.0" encoding="UTF-8"?>`...)
		if opts.Indent != "" {
			w.newline(0)
		}
	}
	return w, nil
}

func (w *Encoder) fail(code, msg string) error {
	if w.err == nil {
		w.err = problem(code, msg)
	}
	return w.err
}

// limit records a terminal limit error naming the option.
func (w *Encoder) limit(name string) error {
	if w.err == nil {
		w.err = apierror.WithDetails(apierror.Invalid, name+" exceeded", attrs.Bag{"code": "limit", "limit": name})
	}
	return w.err
}

// Err returns the terminal error, or nil while the writer is usable or finished cleanly.
func (w *Encoder) Err() error { return w.err }

// BeginTree starts bounding the output by MaxNodeBytes until EndTree. The pending
// start tag of the parent is completed first, so it is not charged to the tree.
func (w *Encoder) BeginTree() {
	if w.err == nil && !w.finished {
		w.openTag()
	}
	w.treeStart = w.written + int64(len(w.buf))
}

// EndTree stops bounding the output.
func (w *Encoder) EndTree() { w.treeStart = -1 }

func (w *Encoder) ready() error {
	if w.err != nil {
		return w.err
	}
	if w.finished {
		return w.fail("state", "writer is finished")
	}
	return nil
}

// Drain transfers ownership of buffered output to the caller.
func (w *Encoder) Drain() []byte {
	p := w.buf
	w.written += int64(len(p))
	w.buf = nil
	return p
}

// Buffered returns output bytes borrowed until the next writer operation.
func (w *Encoder) Buffered() []byte { return w.buf }

// Close releases writer buffers and prevents further writes.
func (w *Encoder) Close() {
	w.finished = true
	w.buf = nil
	w.stack = nil
	w.ns.reset()
	w.attributes = nil
	w.values = nil
	w.order = nil
	w.valid = [recentNames]string{}
}

func (w *Encoder) openTag() {
	if len(w.stack) > 0 && w.stack[len(w.stack)-1].pending {
		w.buf = append(w.buf, '>')
		w.stack[len(w.stack)-1].pending = false
	}
}

// contentBytes returns the bytes content would write: the parent's pending bracket and,
// before a child element, the indentation.
func (w *Encoder) contentBytes(text bool) int64 {
	if len(w.stack) == 0 {
		return 0
	}
	f := &w.stack[len(w.stack)-1]
	n := int64(0)
	if f.pending {
		n = 1
	}
	if !text && !f.text && w.opts.Indent != "" {
		n += 1 + int64(len(w.stack))*int64(len(w.opts.Indent))
	}
	return n
}

// pendingBytes returns the bytes openTag would write.
func (w *Encoder) pendingBytes() int64 {
	if len(w.stack) > 0 && w.stack[len(w.stack)-1].pending {
		return 1
	}
	return 0
}

// content closes a pending start tag and applies the parent's indentation rule.
func (w *Encoder) content(text bool) {
	if len(w.stack) == 0 {
		return
	}
	w.openTag()
	f := &w.stack[len(w.stack)-1]
	if text {
		f.text = true
	} else if !f.text && w.opts.Indent != "" {
		w.newline(len(w.stack))
	}
}

// StartElement opens an element using expanded attribute names.
func (w *Encoder) StartElement(name string, attrs map[string]string, ns string) error {
	err := w.open(name, ns, w.attrValues(attrs), nil, false)
	clear(w.values)
	return err
}

// StartNode opens an element of a complete tree. Declarations are written on the element;
// text reports that the element holds character data, which disables indentation inside it.
func (w *Encoder) StartNode(name, ns string, attrs []AttrValue, declarations []Binding, text bool) error {
	return w.open(name, ns, attrs, declarations, text)
}

func (w *Encoder) attrValues(attrs map[string]string) []AttrValue {
	values := w.values[:0]
	for k, v := range attrs {
		values = append(values, AttrValue{Key: k, Value: v})
	}
	w.values = values
	return values
}

func (w *Encoder) open(name, ns string, attrs []AttrValue, declarations []Binding, text bool) error {
	if err := w.ready(); err != nil {
		return err
	}
	if int64(len(name)) >= w.maxTag {
		return w.limit("max_tag_bytes")
	}
	if !w.knownName(name) {
		return w.fail("element", "invalid element name")
	}
	if ns == xmlnsNamespace {
		return w.fail("namespace", "reserved element namespace")
	}
	if len(w.stack) == 0 && w.root {
		return w.fail("state", "multiple root elements")
	}
	if len(w.stack) >= w.opts.MaxDepth {
		return w.limit("max_depth")
	}
	if len(attrs)+len(declarations) > w.opts.MaxAttrs {
		return w.limit("max_attrs")
	}
	scope := w.ns.len()
	if err := w.declareRoot(scope); err != nil {
		return err
	}
	for _, b := range declarations {
		if err := validateOutputBinding(b.Prefix, b.URI); err != nil {
			w.err = err
			return err
		}
		if b.Prefix != "xml" {
			if err := w.declare(b.Prefix, b.URI, scope); err != nil {
				return err
			}
		}
	}
	as := w.attributes[:0]
	for _, a := range attrs {
		u, n, err := w.expanded(a.Key)
		if err != nil {
			w.err = err
			return err
		}
		as = append(as, outputAttribute{name: n, ns: u, value: a.Value})
	}
	return w.startPrepared(name, ns, scope, as, false, text)
}

func (w *Encoder) startPrepared(name, ns string, scope int, as []outputAttribute, trusted, text bool) error {
	var prefix string
	var err error
	if len(w.stack) > 0 && w.ns.len() == scope && w.stack[len(w.stack)-1].ns == ns {
		prefix = w.stack[len(w.stack)-1].prefix
	} else if ns == "" {
		if w.binding("") != "" {
			err = w.declare("", "", scope)
		}
	} else {
		prefix, err = w.prefix(ns, false, scope)
	}
	if err != nil {
		return err
	}

	less := func(a, b outputAttribute) int {
		if c := strings.Compare(a.ns, b.ns); c != 0 {
			return c
		}
		return strings.Compare(a.name, b.name)
	}
	if len(as) > 1 && !slices.IsSortedFunc(as, less) {
		slices.SortFunc(as, less)
	}
	w.attributes = as
	for i := range as {
		a := &as[i]
		a.prefix = ""
		if a.ns != "" {
			if a.prefix, err = w.prefix(a.ns, true, scope); err != nil {
				return err
			}
		}
	}
	if len(as)+w.ns.len()-scope > w.opts.MaxAttrs {
		return w.limit("max_attrs")
	}
	w.order = append(w.order[:0], w.ns.list[scope:]...)
	if len(w.order) > 1 {
		slices.SortFunc(w.order, func(a, b Binding) int { return strings.Compare(a.Prefix, b.Prefix) })
	}
	head := int64(len("<")) + qualifiedLen(prefix, name)
	pre := int64(0)
	if w.treeStart >= 0 {
		pre = w.contentBytes(false)
	}
	tagRoom := w.maxTag - head - int64(len(">"))
	nodeRoom := w.nodeRoom() - pre - head
	if tagRoom < 0 {
		return w.limit("max_tag_bytes")
	}
	if nodeRoom < 0 {
		return w.limit("max_node_bytes")
	}
	// The worst case of the whole tag decides whether each part needs its own check.
	careful := false
	for _, b := range w.order {
		if careful = !reserveWorst(&tagRoom, &nodeRoom, declarationLen(b.Prefix), int64(len(b.URI))); careful {
			break
		}
	}
	for i := 0; i < len(as) && !careful; i++ {
		a := &as[i]
		careful = !reserveWorst(&tagRoom, &nodeRoom, attributeLen(a.prefix, a.name), int64(len(a.value))+int64(len(a.data)))
	}
	w.content(false)
	tag := len(w.buf)
	w.reserve(int(head) + startTagReserve)
	w.buf = append(w.buf, '<')
	w.name(prefix, name)
	for _, b := range w.order {
		if careful {
			if err := admit(w, tag, declarationLen(b.Prefix), b.URI, true); err != nil {
				return err
			}
		}
		w.buf = append(w.buf, " xmlns"...)
		if b.Prefix != "" {
			w.buf = append(w.buf, ':')
			w.buf = append(w.buf, b.Prefix...)
		}
		w.buf = append(w.buf, '=', '"')
		if err := escape(w, b.URI, true, trusted); err != nil {
			return err
		}
		w.buf = append(w.buf, '"')
	}
	clear(w.order)
	for _, a := range as {
		var err error
		if careful {
			fixed := attributeLen(a.prefix, a.name)
			if trusted {
				err = admit(w, tag, fixed, a.data, true)
			} else {
				err = admit(w, tag, fixed, a.value, true)
			}
			if err != nil {
				return err
			}
		}
		w.buf = append(w.buf, ' ')
		w.name(a.prefix, a.name)
		w.buf = append(w.buf, '=', '"')
		if trusted {
			err = escape(w, a.data, true, true)
		} else {
			err = escape(w, a.value, true, false)
		}
		if err != nil {
			return err
		}
		w.buf = append(w.buf, '"')
	}
	clear(w.attributes)
	w.root = true
	w.stack = append(w.stack, outputFrame{name: name, ns: ns, prefix: prefix, scope: scope, tag: int64(len(w.buf) - tag), pending: true, text: text})
	return nil
}

// EndElement closes the current element, using an empty tag when it has no content.
func (w *Encoder) EndElement() error {
	if err := w.ready(); err != nil {
		return err
	}
	if len(w.stack) == 0 {
		return w.fail("state", "end requires an open element")
	}
	f := w.stack[len(w.stack)-1]
	if f.pending {
		if f.tag+int64(len("/>")) > w.maxTag {
			return w.limit("max_tag_bytes")
		}
		if w.exceeds(int64(len("/>"))) {
			return w.limit("max_node_bytes")
		}
		w.buf = append(w.buf, "/>"...)
	} else {
		indent := !f.text && w.opts.Indent != ""
		if w.treeStart >= 0 {
			n := int64(len("</>")) + qualifiedLen(f.prefix, f.name)
			if indent {
				n += 1 + int64(len(w.stack)-1)*int64(len(w.opts.Indent))
			}
			if w.exceeds(n) {
				return w.limit("max_node_bytes")
			}
		}
		if indent {
			w.newline(len(w.stack) - 1)
		}
		w.buf = append(w.buf, "</"...)
		w.name(f.prefix, f.name)
		w.buf = append(w.buf, '>')
	}
	w.stack[len(w.stack)-1] = outputFrame{}
	w.stack = w.stack[:len(w.stack)-1]
	w.ns.truncate(f.scope)
	return nil
}

// Text writes validated character data.
func (w *Encoder) Text(data string) error {
	if err := w.ready(); err != nil {
		return err
	}
	if len(w.stack) == 0 {
		return w.fail("state", "text requires an open element")
	}
	if data == "" {
		return nil
	}
	if err := admitText(w, data); err != nil {
		return err
	}
	w.content(true)
	return w.escaped(data, false)
}

// Comment writes a validated XML comment.
func (w *Encoder) Comment(data string) error {
	if err := w.ready(); err != nil {
		return err
	}
	if err := w.admitMarkup(int64(len("<!---->")), int64(len(data))); err != nil {
		return err
	}
	if strings.Contains(data, "--") || strings.HasSuffix(data, "-") || !utf8.ValidString(data) {
		return w.fail("syntax", "invalid comment")
	}
	for _, r := range data {
		if !validChar(r) {
			return w.fail("syntax", "invalid comment character")
		}
	}
	w.openTag()
	w.buf = append(w.buf, "<!--"...)
	w.buf = append(w.buf, data...)
	w.buf = append(w.buf, "-->"...)
	return nil
}

func (w *Encoder) instruction(target string, data []byte) error {
	if err := w.ready(); err != nil {
		return err
	}
	fixed := int64(len("<??>")) + int64(len(target))
	if len(data) > 0 {
		fixed += int64(len(" "))
	}
	if err := w.admitMarkup(fixed, int64(len(data))); err != nil {
		return err
	}
	w.openTag()
	w.buf = append(w.buf, "<?"...)
	w.buf = append(w.buf, target...)
	if len(data) > 0 {
		w.buf = append(w.buf, ' ')
		w.buf = append(w.buf, data...)
	}
	w.buf = append(w.buf, "?>"...)
	return nil
}

func (w *Encoder) newline(depth int) {
	w.buf = append(w.buf, '\n')
	for range depth {
		w.buf = append(w.buf, w.opts.Indent...)
	}
}

// Finish completes a document after checking that its root is closed.
func (w *Encoder) Finish() error {
	if w.err != nil {
		return w.err
	}
	if w.finished {
		return w.fail("state", "writer is finished")
	}
	if !w.root || len(w.stack) > 0 {
		return w.fail("state", "unfinished document")
	}
	w.finished = true
	return nil
}
