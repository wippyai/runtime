// SPDX-License-Identifier: MPL-2.0

package xml

import (
	"math"

	lua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/system/xml"
)

// optionTable returns the options table at index n, or nil when the argument is absent.
func optionTable(l *lua.LState, n int) (*lua.LTable, *lua.Error) {
	switch v := l.Get(n).(type) {
	case *lua.LNilType:
		return nil, nil
	case *lua.LTable:
		return v, nil
	default:
		return nil, argument(l, "options must be a table")
	}
}

// eachOption visits every option, rejecting non-string keys.
func eachOption(l *lua.LState, t *lua.LTable, visit func(key string, v lua.LValue) *lua.Error) *lua.Error {
	if t == nil {
		return nil
	}
	var failure *lua.Error
	for k, v := t.Next(lua.LNil); k != lua.LNil; k, v = t.Next(k) {
		key, ok := k.(lua.LString)
		if !ok {
			return argument(l, "option names must be strings")
		}
		if failure = visit(string(key), v); failure != nil {
			return failure
		}
	}
	return nil
}

func integerOption(l *lua.LState, key string, v lua.LValue, minimum int64) (int64, *lua.Error) {
	var n float64
	switch x := v.(type) {
	case lua.LInteger:
		n = float64(x)
	case lua.LNumber:
		n = float64(x)
	default:
		return 0, argument(l, key+" must be an integer")
	}
	maximum := float64(math.MaxInt32)
	if key == "max_bytes" {
		maximum = 1 << 53
	}
	if n != math.Trunc(n) || n < float64(minimum) || n > maximum {
		return 0, argument(l, key+" is out of range")
	}
	return int64(n), nil
}

func boolOption(l *lua.LState, key string, v lua.LValue) (bool, *lua.Error) {
	b, ok := v.(lua.LBool)
	if !ok {
		return false, argument(l, key+" must be a boolean")
	}
	return bool(b), nil
}

// stringMapOption reads a table of string keys and string values.
func stringMapOption(l *lua.LState, key string, v lua.LValue) (map[string]string, *lua.Error) {
	t, ok := v.(*lua.LTable)
	if !ok {
		return nil, argument(l, key+" must be a table of strings")
	}
	out := make(map[string]string)
	for k, val := range tablePairs(t) {
		ks, ok1 := k.(lua.LString)
		vs, ok2 := val.(lua.LString)
		if !ok1 || !ok2 {
			return nil, argument(l, key+" must be a table of strings")
		}
		out[string(ks)] = string(vs)
	}
	return out, nil
}

// tablePairs iterates the raw entries of t.
func tablePairs(t *lua.LTable) func(func(lua.LValue, lua.LValue) bool) {
	return func(yield func(lua.LValue, lua.LValue) bool) {
		for k, v := t.Next(lua.LNil); k != lua.LNil; k, v = t.Next(k) {
			if !yield(k, v) {
				return
			}
		}
	}
}

// readOptions parses reader and decode options.
func readOptions(l *lua.LState, n int) (xml.Options, *lua.Error) {
	var o xml.Options
	t, err := optionTable(l, n)
	if err != nil {
		return o, err
	}
	err = eachOption(l, t, func(key string, v lua.LValue) *lua.Error {
		var failure *lua.Error
		var limit int64
		switch key {
		case "whitespace":
			s, ok := v.(lua.LString)
			if !ok || s != "preserve" && s != "strip" {
				return argument(l, `whitespace must be "preserve" or "strip"`)
			}
			o.Whitespace = string(s)
		case "comments":
			o.Comments, failure = boolOption(l, key, v)
		case "instructions":
			o.Instructions, failure = boolOption(l, key, v)
		case "max_bytes":
			o.MaxBytes, failure = integerOption(l, key, v, 0)
		case "max_depth", "max_attrs", "max_namespaces", "max_tag_bytes", "max_token_bytes", "max_node_bytes":
			limit, failure = integerOption(l, key, v, 1)
			switch key {
			case "max_depth":
				o.MaxDepth = int(limit)
			case "max_attrs":
				o.MaxAttrs = int(limit)
			case "max_namespaces":
				o.MaxNamespaces = int(limit)
			case "max_tag_bytes":
				o.MaxTagBytes = int(limit)
			case "max_token_bytes":
				o.MaxTokenBytes = int(limit)
			case "max_node_bytes":
				o.MaxNodeBytes = int(limit)
			}
		default:
			return argument(l, "unknown option "+key)
		}
		return failure
	})
	return o, err
}

// writeOptions parses writer and encode options.
func writeOptions(l *lua.LState, n int) (xml.EncodeOptions, *lua.Error) {
	var o xml.EncodeOptions
	t, err := optionTable(l, n)
	if err != nil {
		return o, err
	}
	err = eachOption(l, t, func(key string, v lua.LValue) *lua.Error {
		var failure *lua.Error
		switch key {
		case "indent":
			s, ok := v.(lua.LString)
			if !ok {
				return argument(l, "indent must be a string")
			}
			o.Indent = string(s)
		case "declaration":
			o.Declaration, failure = boolOption(l, key, v)
		case "ns":
			o.NS, failure = stringMapOption(l, key, v)
		case "max_depth", "max_attrs", "max_namespaces", "max_node_bytes", "max_tag_bytes":
			var limit int64
			limit, failure = integerOption(l, key, v, 1)
			switch key {
			case "max_depth":
				o.MaxDepth = int(limit)
			case "max_attrs":
				o.MaxAttrs = int(limit)
			case "max_namespaces":
				o.MaxNamespaces = int(limit)
			case "max_node_bytes":
				o.MaxNodeBytes = int(limit)
			case "max_tag_bytes":
				o.MaxTagBytes = int(limit)
			}
		default:
			return argument(l, "unknown option "+key)
		}
		return failure
	})
	return o, err
}

// selectOptions parses the namespace bindings used by paths.
func selectOptions(l *lua.LState, n int) (map[string]string, *lua.Error) {
	var ns map[string]string
	t, err := optionTable(l, n)
	if err != nil {
		return nil, err
	}
	err = eachOption(l, t, func(key string, v lua.LValue) *lua.Error {
		if key != "ns" {
			return argument(l, "unknown option "+key)
		}
		var failure *lua.Error
		ns, failure = stringMapOption(l, key, v)
		return failure
	})
	return ns, err
}
