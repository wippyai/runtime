// SPDX-License-Identifier: MPL-2.0
// Package xml provides streaming XML 1.0 parsing, selection and serialization.
package xml

import (
	"strings"
	"unicode/utf8"

	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

// XMLNamespace identifies the implicit xml namespace.
const XMLNamespace = "http://www.w3.org/XML/1998/namespace"

const xmlnsNamespace = "http://www.w3.org/2000/xmlns/"

// Options configures decoding limits and event reporting.
type Options struct {
	// Whitespace selects preserve or strip behavior for literal whitespace runs.
	Whitespace string
	// MaxBytes limits total UTF-8 input bytes; zero leaves the document size unlimited.
	MaxBytes int64
	// MaxDepth limits the number of open elements.
	MaxDepth int
	// MaxAttrs limits attributes and declarations on one start tag.
	MaxAttrs int
	// MaxTagBytes limits one start tag or reported comment or instruction.
	MaxTagBytes int
	// MaxTokenBytes limits the UTF-8 bytes in one text event.
	MaxTokenBytes int
	// MaxNodeBytes limits the source span of a materialized element.
	MaxNodeBytes int
	// MaxNamespaces limits the declarations in scope at once, shadowed ones included.
	MaxNamespaces int
	// Comments enables comment events.
	Comments bool
	// Instructions enables processing-instruction events.
	Instructions bool
	strip        bool
}

func (o Options) defaults() (Options, error) {
	if o.Whitespace == "" {
		o.Whitespace = "preserve"
	}
	if o.Whitespace != "preserve" && o.Whitespace != "strip" {
		return o, problem("argument", "invalid whitespace option")
	}
	o.strip = o.Whitespace == "strip"
	if o.MaxTagBytes > int(^uint(0)>>1)-bufferLookahead || o.MaxTokenBytes > int(^uint(0)>>1)-bufferLookahead {
		return o, problem("argument", "limit is too large")
	}
	if o.MaxDepth < 0 || o.MaxAttrs < 0 || o.MaxTagBytes < 0 || o.MaxTokenBytes < 0 || o.MaxNodeBytes < 0 || o.MaxNamespaces < 0 || o.MaxBytes < 0 {
		return o, problem("argument", "limits must be nonnegative")
	}
	if o.MaxDepth == 0 {
		o.MaxDepth = defaultMaxDepth
	}
	if o.MaxAttrs == 0 {
		o.MaxAttrs = defaultMaxAttrs
	}
	if o.MaxTagBytes == 0 {
		o.MaxTagBytes = defaultMaxTokenBytes
	}
	if o.MaxTokenBytes == 0 {
		o.MaxTokenBytes = defaultMaxTokenBytes
	}
	if o.MaxNodeBytes == 0 {
		o.MaxNodeBytes = defaultMaxNodeBytes
	}
	if o.MaxNamespaces == 0 {
		o.MaxNamespaces = defaultMaxNamespaces
	}
	return o, nil
}

// Element represents an XML element in normalized tree form.
type Element struct {
	// Name is the local element name.
	Name string
	// NS is the resolved namespace URI.
	NS string
	// Attrs maps expanded attribute names to values.
	Attrs map[string]string
	// XMLNS holds declarations, or the complete scope on a materialized root.
	XMLNS map[string]string
	// Text holds text-only content.
	Text string
	// Children holds ordered strings and child elements for mixed or element content.
	Children []any
}

// Kind identifies a cursor event type.
type Kind uint8

const (
	// StartEvent opens an element.
	StartEvent Kind = iota + 1
	// EndEvent closes an element.
	EndEvent
	// TextEvent carries normalized character data.
	TextEvent
	// CommentEvent carries a comment.
	CommentEvent
	// PIEvent carries a processing instruction.
	PIEvent
)

// Position locates a UTF-8 byte offset with one-based line and character column.
type Position struct {
	// Offset counts UTF-8 bytes from the start of the document.
	Offset int64
	// Line is the one-based line number.
	Line int
	// Column is the one-based character column.
	Column int
}

// Attribute holds an expanded name and a borrowed normalized value.
type Attribute struct {
	// Name is the local attribute name.
	Name string
	// NS is the resolved namespace URI.
	NS string
	// Value borrows normalized bytes until the next decoder advance.
	Value []byte
}

// Binding associates a prefix with a namespace URI.
type Binding struct {
	// Prefix is the namespace prefix, or empty for the default namespace.
	Prefix string
	// URI is the bound namespace URI.
	URI string
}

// Event slices remain valid until the next decoder advance.
type Event struct {
	// Name is the local element name or processing-instruction target.
	Name string
	// NS is the resolved element namespace URI.
	NS string
	// Text borrows normalized character data.
	Text []byte
	// Attrs borrows the attributes of a start event.
	Attrs []Attribute
	// XMLNS borrows declarations on a start event.
	XMLNS []Binding
	// Pos locates the beginning of the event.
	Pos Position
	// EndOffset is the UTF-8 offset immediately after the event.
	EndOffset int64
	// Depth counts open elements after the event.
	Depth int
	// Kind identifies the event type.
	Kind Kind
}

func problem(code, message string) error {
	return apierror.WithDetails(apierror.Invalid, message, attrs.Bag{"code": code})
}

func validChar(r rune) bool {
	return r == 9 || r == 10 || r == 13 || r >= 0x20 && r <= 0xD7FF || r >= 0xE000 && r <= 0xFFFD || r >= 0x10000 && r <= 0x10FFFF
}

func nameStart(r rune) bool {
	return r == '_' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r >= 0xC0 && r <= 0xD6 || r >= 0xD8 && r <= 0xF6 || r >= 0xF8 && r <= 0x2FF || r >= 0x370 && r <= 0x37D || r >= 0x37F && r <= 0x1FFF || r >= 0x200C && r <= 0x200D || r >= 0x2070 && r <= 0x218F || r >= 0x2C00 && r <= 0x2FEF || r >= 0x3001 && r <= 0xD7FF || r >= 0xF900 && r <= 0xFDCF || r >= 0xFDF0 && r <= 0xFFFD || r >= 0x10000 && r <= 0xEFFFF
}

func nameChar(r rune) bool {
	return nameStart(r) || r == '-' || r == '.' || r >= '0' && r <= '9' || r == 0xB7 || r >= 0x300 && r <= 0x36F || r >= 0x203F && r <= 0x2040
}

func validName(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); {
		r := rune(s[i])
		k := 1
		if r >= 128 {
			r, k = utf8.DecodeRuneInString(s[i:])
			if r == utf8.RuneError && k == 1 {
				return false
			}
		}
		if i == 0 {
			if !nameStart(r) {
				return false
			}
		} else if !nameChar(r) {
			return false
		}
		i += k
	}
	return true
}

func qname(s string) (string, string, error) {
	if !utf8.ValidString(s) {
		return "", "", problem("encoding", "malformed UTF-8 name")
	}
	p, n, ok := strings.Cut(s, ":")
	if !ok {
		if !validName(s) {
			return "", "", problem("syntax", "invalid XML name")
		}
		return "", s, nil
	}
	if strings.Contains(n, ":") || !validName(p) || !validName(n) {
		return "", "", problem("namespace", "invalid QName")
	}
	// The parts are copied so that a short local name never keeps a long QName alive.
	return strings.Clone(p), strings.Clone(n), nil
}

func space(b byte) bool { return b == ' ' || b == '\t' || b == '\n' || b == '\r' }

func white(p []byte) bool {
	for _, b := range p {
		if !space(b) {
			return false
		}
	}
	return true
}

// AttrKey returns the attribute key used in Element.Attrs: the local name, or {uri}local.
func AttrKey(ns, name string) string {
	if ns == "" {
		return name
	}
	return "{" + ns + "}" + name
}
