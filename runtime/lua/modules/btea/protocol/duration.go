package protocol

import (
	"fmt"
	lua "github.com/yuin/gopher-lua"
	"strconv"
	"strings"
	"time"
)

// ParseDuration converts a Lua value to a time.Duration.
// It handles:
// - numbers (interpreted as milliseconds)
// - strings (parsed as Go duration strings like "1s", "500ms", "2m")
func ParseDuration(value lua.LValue) (time.Duration, error) {
	switch v := value.(type) {
	case lua.LNumber:
		// If it's a number, interpret as milliseconds
		return time.Duration(v) * time.Millisecond, nil
	case lua.LString:
		str := string(v)
		// If the string only contains digits, treat as milliseconds
		if _, err := strconv.Atoi(str); err == nil {
			ms, _ := strconv.ParseInt(str, 10, 64)
			return time.Duration(ms) * time.Millisecond, nil
		}
		// Otherwise parse as duration string
		return parseDurationString(str)
	default:
		return 0, fmt.Errorf("duration must be a number (milliseconds) or string (e.g., '1s'), got %s", value.Type())
	}
}

// parseDurationString parses a duration string with support for:
// ms, s, m, h units
func parseDurationString(s string) (time.Duration, error) {
	// First try Go's built-in parser
	if d, err := time.ParseDuration(s); err == nil {
		return d, nil
	}

	// Handle special cases and normalize input
	s = strings.ToLower(strings.TrimSpace(s))

	// Handle special case where there might be a space between number and unit
	s = strings.ReplaceAll(s, " ", "")

	// Try parsing with Go's parser again after normalization
	if d, err := time.ParseDuration(s); err == nil {
		return d, nil
	}

	return 0, fmt.Errorf("invalid duration format: %s", s)
}

// CheckDuration gets a duration from Lua state at the given index
// For convenience in model implementations
func CheckDuration(l *lua.LState, index int) time.Duration {
	value := l.Get(index)
	duration, err := ParseDuration(value)
	if err != nil {
		l.ArgError(index, err.Error())
		return 0
	}
	return duration
}

// PushDuration pushes a duration value to Lua state
// It converts the duration to milliseconds for consistency
func PushDuration(l *lua.LState, d time.Duration) {
	l.Push(lua.LNumber(d.Milliseconds()))
}
