package httpclient

import (
	"errors"
	"net/url"
	"strings"

	lua "github.com/yuin/gopher-lua"
)

func encodeURI(l *lua.LState) int {
	// check num args
	if l.GetTop() != 1 {
		l.ArgError(1, "encode_uri requires exactly 1 string argument")
		return 0
	}

	// check type
	if l.Get(1).Type() != lua.LTString {
		l.ArgError(1, "string expected")
		return 0
	}

	str := l.CheckString(1) // Correctly checks if the first argument is a string.

	l.Push(lua.LString(url.QueryEscape(str)))
	return 1
}

func decodeURI(l *lua.LState) int {
	// check num args
	if l.GetTop() != 1 {
		l.ArgError(1, "decode_uri requires exactly 1 string argument")
		return 0
	}

	// check type
	if l.Get(1).Type() != lua.LTString {
		l.ArgError(1, "string expected")
		return 0
	}

	str := l.CheckString(1) // Correctly checks if the first argument is a string.
	decoded, err := url.QueryUnescape(str)
	if err != nil {
		// This is a processing error, not an argument error
		l.Push(lua.LNil)
		l.Push(newHTTPIOError(l, err, "url_decode"))
		return 2
	}
	l.Push(lua.LString(decoded))
	return 1
}

// getMethodFromArgs fetches and validates the HTTP method from Lua arguments.
func getMethodFromArgs(l *lua.LState, argIndex int) (string, error) {
	method := l.CheckString(argIndex)
	if method == "" {
		return "", errors.New("method cannot be empty")
	}

	// validate method
	switch strings.ToUpper(method) {
	case "GET", "POST", "PUT", "DELETE", "HEAD", "PATCH":
		return strings.ToUpper(method), nil
	default:
		return "", errors.New("invalid method")
	}
}

// getURLFromArgs fetches and validates the URL from Lua arguments.
func getURLFromArgs(l *lua.LState, argIndex int) (string, error) {
	if l.Get(argIndex).Type() != lua.LTString {
		return "", errors.New("URL must be a string")
	}

	urlString := l.CheckString(argIndex)
	if urlString == "" {
		return "", errors.New("URL cannot be empty")
	}

	return urlString, nil
}

// getOptionsFromArgs fetches and validates the options from Lua arguments.
func getOptionsFromArgs(l *lua.LState, argIndex int) (*requestOptions, error) {
	optionsValue := l.OptTable(argIndex, l.CreateTable(0, 3))
	opts, err := parseOptions(optionsValue)
	if err != nil {
		return nil, err
	}

	return opts, nil
}
