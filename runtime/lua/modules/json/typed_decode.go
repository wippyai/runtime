// SPDX-License-Identifier: MPL-2.0

package json

import (
	"errors"
	"fmt"
	"strconv"
	"sync"
	"unicode/utf8"
	"unsafe"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/go-lua/types/kind"
	"github.com/wippyai/go-lua/types/typ"
)

// A plan only contains shapes whose validation is completely determined by
// the same typ nodes used by LType.Validate. Everything else uses that oracle.
type decodePlan struct {
	t        typ.Type
	elem     *decodePlan
	byName   map[string]int
	fields   []fieldPlan
	variants []*decodePlan
	kind     kind.Kind
}
type fieldPlan struct {
	plan     *decodePlan
	name     string
	optional bool
}

var planCache = struct {
	entries map[*lua.LType]*decodePlan
	order   []*lua.LType
	sync.Mutex
}{entries: make(map[*lua.LType]*decodePlan)}

const maxCachedPlans = 256

func planFor(t *lua.LType) *decodePlan {
	planCache.Lock()
	defer planCache.Unlock()
	if p, ok := planCache.entries[t]; ok {
		return p
	}
	p := compilePlan(t.Inner(), 0)
	if len(planCache.order) == maxCachedPlans {
		delete(planCache.entries, planCache.order[0])
		copy(planCache.order, planCache.order[1:])
		planCache.order = planCache.order[:maxCachedPlans-1]
	}
	planCache.entries[t] = p
	planCache.order = append(planCache.order, t)
	return p
}

func compilePlan(t typ.Type, depth int) *decodePlan {
	if t == nil || depth > 60 {
		return nil
	}
	p := &decodePlan{kind: t.Kind(), t: t}
	switch tt := t.(type) {
	case *typ.Annotated:
		// Kind reports the underlying shape, but only the runtime validator
		// evaluates annotations such as min, max, and pattern.
		return nil
	case *typ.Record:
		if tt.Open || tt.HasMapComponent() || tt.Metatable != nil || len(tt.Fields) > 64 {
			return nil
		}
		p.fields = make([]fieldPlan, len(tt.Fields))
		p.byName = make(map[string]int, len(tt.Fields))
		for i, f := range tt.Fields {
			child := compilePlan(f.Type, depth+1)
			if child == nil {
				return nil
			}
			p.fields[i] = fieldPlan{plan: child, name: f.Name, optional: f.Optional}
			p.byName[f.Name] = i
		}
	case *typ.Array:
		p.elem = compilePlan(tt.Element, depth+1)
		if p.elem == nil {
			return nil
		}
	case *typ.Map:
		key := compilePlan(tt.Key, depth+1)
		if key == nil || key.kind != kind.String {
			return nil
		}
		p.elem = compilePlan(tt.Value, depth+1)
		if p.elem == nil {
			return nil
		}
	case *typ.Optional:
		p.elem = compilePlan(tt.Inner, depth+1)
		if p.elem == nil {
			return nil
		}
	case *typ.Union:
		for _, member := range tt.Members {
			child := compilePlan(member, depth+1)
			if child == nil || !child.scalar() {
				return nil
			}
			p.variants = append(p.variants, child)
		}
	case *typ.Literal:
		if tt.Base != kind.String && tt.Base != kind.Boolean && tt.Base != kind.Integer && tt.Base != kind.Number {
			return nil
		}
	default:
		switch t.Kind() {
		case kind.Nil, kind.Boolean, kind.Number, kind.Integer, kind.String:
		default:
			return nil
		}
	}
	return p
}
func (p *decodePlan) scalar() bool {
	if p.kind == kind.Optional {
		return p.elem.scalar()
	}
	return p.kind != kind.Array && p.kind != kind.Map && p.kind != kind.Record
}

var errTypedFallback = errors.New("typed decode fallback")

// DecodeTyped implements json.decode(data, target). Its fallback is exactly
// Decode followed by the runtime validator used by T:is.
func DecodeTyped(data []byte, target *lua.LType, l *lua.LState) (lua.LValue, error) {
	return decodeTyped(data, target, l, false)
}

func decodeTyped(data []byte, target *lua.LType, l *lua.LState, immutable bool) (lua.LValue, error) {
	if target == nil {
		return lua.LNil, errors.New("$: expected type value")
	}
	p := planFor(target)
	var fastErr error
	if p != nil {
		owned := data
		if !immutable {
			// Every escape-free string can then share this one stable backing
			// allocation, even if the caller reuses its input buffer.
			owned = append([]byte(nil), data...)
		}
		s := typedScanner{data: owned}
		v, err := s.value(p, 0)
		fastErr = err
		if err == nil {
			s.space()
			if s.off == len(data) {
				return v, nil
			}
		}
	}
	v, err := Decode(data)
	if err != nil {
		return lua.LNil, fmt.Errorf("$: %w", err)
	}
	if !target.Validate(l, v) {
		if fastErr != nil && !errors.Is(fastErr, errTypedFallback) {
			return lua.LNil, fastErr
		}
		return lua.LNil, fmt.Errorf("$: expected %s", target)
	}
	return v, nil
}

func decodeTypedString(raw string, target *lua.LType, l *lua.LState) (lua.LValue, error) {
	return decodeTyped(unsafe.Slice(unsafe.StringData(raw), len(raw)), target, l, true)
}

type typedScanner struct {
	data    []byte
	path    [128]pathPart
	off     int
	pathLen int
}
type pathPart struct {
	name  string
	index int
}

func (s *typedScanner) expected(t typ.Type) error {
	path := "$"
	for i := 0; i < s.pathLen; i++ {
		if s.path[i].name != "" {
			path += "." + s.path[i].name
		} else {
			path += "[" + strconv.Itoa(s.path[i].index) + "]"
		}
	}
	return fmt.Errorf("%s: expected %s", path, t)
}

func (s *typedScanner) space() {
	for s.off < len(s.data) {
		switch s.data[s.off] {
		case ' ', '\n', '\r', '\t':
			s.off++
		default:
			return
		}
	}
}
func (s *typedScanner) take(c byte) bool {
	s.space()
	if s.off < len(s.data) && s.data[s.off] == c {
		s.off++
		return true
	}
	return false
}
func (s *typedScanner) literal(v string) bool {
	s.space()
	if len(s.data)-s.off < len(v) {
		return false
	}
	for i := range v {
		if s.data[s.off+i] != v[i] {
			return false
		}
	}
	s.off += len(v)
	return true
}
func (s *typedScanner) stringValue() (string, error) {
	s.space()
	if s.off >= len(s.data) || s.data[s.off] != '"' {
		return "", errTypedFallback
	}
	s.off++
	start := s.off
	nonASCII := false
	for s.off < len(s.data) {
		c := s.data[s.off]
		if c == '"' {
			if nonASCII && !utf8.Valid(s.data[start:s.off]) {
				return "", errTypedFallback
			}
			v := unsafe.String(unsafe.SliceData(s.data[start:s.off]), s.off-start)
			s.off++
			return v, nil
		}
		if c == '\\' {
			return s.escapedString(start)
		}
		if c < 0x20 {
			return "", errTypedFallback
		}
		if c >= 0x80 {
			nonASCII = true
		}
		s.off++
	}
	return "", errTypedFallback
}
func (s *typedScanner) escapedString(start int) (string, error) {
	// Size the output once. LLM payloads often contain many escapes, and
	// growing this buffer for each segment dominates their allocation cost.
	end := s.off
	for end < len(s.data) {
		if s.data[end] == '\\' {
			end += 2
			continue
		}
		if s.data[end] == '"' {
			break
		}
		end++
	}
	if end >= len(s.data) {
		return "", errTypedFallback
	}
	out := make([]byte, 0, end-start)
	segment := start
	for s.off < len(s.data) {
		c := s.data[s.off]
		if c == '"' {
			out = append(out, s.data[segment:s.off]...)
			if !utf8.Valid(out) {
				return "", errTypedFallback
			}
			s.off++
			return string(out), nil
		}
		if c < 0x20 {
			return "", errTypedFallback
		}
		if c != '\\' {
			s.off++
			continue
		}
		out = append(out, s.data[segment:s.off]...)
		s.off++
		if s.off == len(s.data) {
			return "", errTypedFallback
		}
		switch s.data[s.off] {
		case '"', '\\', '/':
			out = append(out, s.data[s.off])
		case 'b':
			out = append(out, '\b')
		case 'f':
			out = append(out, '\f')
		case 'n':
			out = append(out, '\n')
		case 'r':
			out = append(out, '\r')
		case 't':
			out = append(out, '\t')
		default:
			return "", errTypedFallback
		}
		s.off++
		segment = s.off
	}
	return "", errTypedFallback
}
func (s *typedScanner) number() (lua.LValue, error) {
	s.space()
	start := s.off
	if s.off < len(s.data) && s.data[s.off] == '-' {
		s.off++
	}
	if s.off == len(s.data) {
		return nil, errTypedFallback
	}
	if s.data[s.off] == '0' {
		s.off++
	} else {
		if s.data[s.off] < '1' || s.data[s.off] > '9' {
			return nil, errTypedFallback
		}
		for s.off < len(s.data) && s.data[s.off] >= '0' && s.data[s.off] <= '9' {
			s.off++
		}
	}
	float := false
	if s.off < len(s.data) && s.data[s.off] == '.' {
		float = true
		s.off++
		begin := s.off
		for s.off < len(s.data) && s.data[s.off] >= '0' && s.data[s.off] <= '9' {
			s.off++
		}
		if begin == s.off {
			return nil, errTypedFallback
		}
	}
	if s.off < len(s.data) && (s.data[s.off] == 'e' || s.data[s.off] == 'E') {
		float = true
		s.off++
		if s.off < len(s.data) && (s.data[s.off] == '+' || s.data[s.off] == '-') {
			s.off++
		}
		begin := s.off
		for s.off < len(s.data) && s.data[s.off] >= '0' && s.data[s.off] <= '9' {
			s.off++
		}
		if begin == s.off {
			return nil, errTypedFallback
		}
	}
	raw := unsafe.String(unsafe.SliceData(s.data[start:s.off]), s.off-start)
	if !float {
		if n, err := strconv.ParseInt(raw, 10, 64); err == nil {
			return lua.LInteger(n), nil
		}
	}
	n, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return nil, errTypedFallback
	}
	return lua.LNumber(n), nil
}
func (s *typedScanner) scalarValue() (lua.LValue, error) {
	s.space()
	if s.off == len(s.data) {
		return nil, errTypedFallback
	}
	switch s.data[s.off] {
	case 'n':
		if s.literal("null") {
			return lua.LNil, nil
		}
	case 't':
		if s.literal("true") {
			return lua.LTrue, nil
		}
	case 'f':
		if s.literal("false") {
			return lua.LFalse, nil
		}
	case '"':
		v, e := s.stringValue()
		return lua.LString(v), e
	case '-', '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		return s.number()
	}
	return nil, errTypedFallback
}
func scalarMatches(p *decodePlan, v lua.LValue) bool {
	switch p.kind {
	case kind.Nil:
		return v == lua.LNil
	case kind.Boolean:
		_, ok := v.(lua.LBool)
		return ok
	case kind.String:
		_, ok := v.(lua.LString)
		return ok
	case kind.Number:
		switch v.(type) {
		case lua.LInteger, lua.LNumber:
			return true
		}
	case kind.Integer:
		switch n := v.(type) {
		case lua.LInteger:
			return true
		case lua.LNumber:
			return lua.IsIntegerValue(n)
		}
	case kind.Literal:
		lit := p.t.(*typ.Literal)
		switch x := lit.Value.(type) {
		case string:
			y, ok := v.(lua.LString)
			return ok && string(y) == x
		case bool:
			y, ok := v.(lua.LBool)
			return ok && bool(y) == x
		case int64:
			switch y := v.(type) {
			case lua.LInteger:
				return int64(y) == x
			case lua.LNumber:
				return float64(y) == float64(x)
			}
		case float64:
			y, ok := v.(lua.LNumber)
			return ok && float64(y) == x
		}
	case kind.Optional:
		return v == lua.LNil || scalarMatches(p.elem, v)
	case kind.Union:
		for _, child := range p.variants {
			if scalarMatches(child, v) {
				return true
			}
		}
	}
	return false
}
func (s *typedScanner) value(p *decodePlan, depth int) (lua.LValue, error) {
	if depth > maxNestingDepth {
		return nil, errTypedFallback
	}
	if p.scalar() {
		v, err := s.scalarValue()
		if err != nil {
			return nil, err
		}
		if !scalarMatches(p, v) {
			return nil, s.expected(p.t)
		}
		return v, nil
	}
	s.space()
	if s.off < len(s.data) && s.data[s.off] == 'n' {
		return nil, errTypedFallback
	}
	switch p.kind {
	case kind.Optional:
		if s.literal("null") {
			return lua.LNil, nil
		}
		return s.value(p.elem, depth+1)
	case kind.Array:
		if !s.take('[') {
			return nil, s.expected(p.t)
		}
		capacity := 8
		if depth == 0 && len(s.data) > 16000 && p.elem.kind == kind.Record {
			capacity = len(s.data) / 28
		}
		t := &lua.LTable{Metatable: lua.LNil, Array: make([]lua.LValue, 0, capacity)}
		if s.take(']') {
			return t, nil
		}
		for i := 1; ; i++ {
			s.space()
			var v lua.LValue
			var err error
			if s.literal("null") {
				v = lua.LNil
			} else {
				s.path[s.pathLen] = pathPart{index: i}
				s.pathLen++
				v, err = s.value(p.elem, depth+1)
				s.pathLen--
			}
			if err != nil {
				return nil, err
			}
			t.Array = append(t.Array, v)
			if s.take(']') {
				return t, nil
			}
			if !s.take(',') {
				return nil, errTypedFallback
			}
		}
	case kind.Map:
		if !s.take('{') {
			return nil, s.expected(p.t)
		}
		t := &lua.LTable{Metatable: lua.LNil, Strdict: make(map[string]lua.LValue, 8)}
		if s.take('}') {
			return t, nil
		}
		for {
			key, err := s.stringValue()
			if err != nil {
				return nil, err
			}
			if _, exists := t.Strdict[key]; exists {
				return nil, errTypedFallback
			}
			if !s.take(':') {
				return nil, errTypedFallback
			}
			s.path[s.pathLen] = pathPart{name: key}
			s.pathLen++
			v, err := s.value(p.elem, depth+1)
			s.pathLen--
			if err != nil {
				return nil, err
			}
			t.Strdict[key] = v
			if s.take('}') {
				return t, nil
			}
			if !s.take(',') {
				return nil, errTypedFallback
			}
		}
	case kind.Record:
		if !s.take('{') {
			return nil, s.expected(p.t)
		}
		t := &lua.LTable{Metatable: lua.LNil, Strdict: make(map[string]lua.LValue, len(p.fields))}
		var seen uint64
		if !s.take('}') {
			for {
				key, err := s.stringValue()
				if err != nil {
					return nil, err
				}
				i, ok := p.byName[key]
				if !ok || seen&(uint64(1)<<i) != 0 {
					return nil, errTypedFallback
				}
				seen |= uint64(1) << i
				if !s.take(':') {
					return nil, errTypedFallback
				}
				s.path[s.pathLen] = pathPart{name: p.fields[i].name}
				s.pathLen++
				v, err := s.value(p.fields[i].plan, depth+1)
				s.pathLen--
				if err != nil {
					return nil, err
				}
				t.Strdict[p.fields[i].name] = v
				if s.take('}') {
					break
				}
				if !s.take(',') {
					return nil, errTypedFallback
				}
			}
		}
		for i, f := range p.fields {
			if seen&(uint64(1)<<i) == 0 && !f.optional {
				s.path[s.pathLen] = pathPart{name: f.name}
				s.pathLen++
				err := s.expected(f.plan.t)
				s.pathLen--
				return nil, err
			}
		}
		return t, nil
	}
	return nil, errTypedFallback
}
