// SPDX-License-Identifier: MPL-2.0

package tty

import (
	"fmt"

	lua "github.com/wippyai/go-lua"
	ttyapi "github.com/wippyai/runtime/api/tty"
)

// DecodeEvent validates and decodes the canonical Lua TTY event record.
func DecodeEvent(table *lua.LTable) (ttyapi.Event, error) {
	eventType, err := requiredString(table, "type")
	if err != nil {
		return ttyapi.Event{}, err
	}
	event := ttyapi.Event{Type: eventType}

	switch eventType {
	case "key":
		if event.Key, err = requiredString(table, "key"); err != nil {
			return ttyapi.Event{}, err
		}
		if event.KeyType, err = requiredString(table, "key_type"); err != nil {
			return ttyapi.Event{}, err
		}
		if event.Action, err = enumString(table, "action", "press", "release"); err != nil {
			return ttyapi.Event{}, err
		}
		if event.Alt, err = optionalBool(table, "alt"); err != nil {
			return ttyapi.Event{}, err
		}
		if event.Ctrl, err = optionalBool(table, "ctrl"); err != nil {
			return ttyapi.Event{}, err
		}
		if event.Shift, err = optionalBool(table, "shift"); err != nil {
			return ttyapi.Event{}, err
		}
	case "mouse":
		if event.Action, err = enumString(table, "action", "press", "release", "motion", "wheel"); err != nil {
			return ttyapi.Event{}, err
		}
		if event.Button, err = requiredString(table, "button"); err != nil {
			return ttyapi.Event{}, err
		}
		if event.X, err = positiveInteger(table, "x"); err != nil {
			return ttyapi.Event{}, err
		}
		if event.Y, err = positiveInteger(table, "y"); err != nil {
			return ttyapi.Event{}, err
		}
		if event.Alt, err = optionalBool(table, "alt"); err != nil {
			return ttyapi.Event{}, err
		}
		if event.Ctrl, err = optionalBool(table, "ctrl"); err != nil {
			return ttyapi.Event{}, err
		}
		if event.Shift, err = optionalBool(table, "shift"); err != nil {
			return ttyapi.Event{}, err
		}
	case "resize", "start":
		if event.Width, err = positiveInteger(table, "width"); err != nil {
			return ttyapi.Event{}, err
		}
		if event.Height, err = positiveInteger(table, "height"); err != nil {
			return ttyapi.Event{}, err
		}
	case "focus":
		if event.Focused, err = requiredBool(table, "focused"); err != nil {
			return ttyapi.Event{}, err
		}
	case "visibility":
		if event.Visible, err = requiredBool(table, "visible"); err != nil {
			return ttyapi.Event{}, err
		}
	case "paste":
		if event.Paste, err = requiredString(table, "text"); err != nil {
			return ttyapi.Event{}, err
		}
	case "close":
	default:
		return ttyapi.Event{}, fmt.Errorf("unsupported TTY event type %q", eventType)
	}
	return event, nil
}

func requiredString(table *lua.LTable, field string) (string, error) {
	value, ok := table.RawGetString(field).(lua.LString)
	if !ok {
		return "", fmt.Errorf("TTY event %s must be a string", field)
	}
	return string(value), nil
}

func enumString(table *lua.LTable, field string, allowed ...string) (string, error) {
	value, err := requiredString(table, field)
	if err != nil {
		return "", err
	}
	for _, candidate := range allowed {
		if value == candidate {
			return value, nil
		}
	}
	return "", fmt.Errorf("unsupported TTY event %s %q", field, value)
}

func optionalBool(table *lua.LTable, field string) (bool, error) {
	value := table.RawGetString(field)
	if value == lua.LNil {
		return false, nil
	}
	boolean, ok := value.(lua.LBool)
	if !ok {
		return false, fmt.Errorf("TTY event %s must be a boolean", field)
	}
	return bool(boolean), nil
}

func requiredBool(table *lua.LTable, field string) (bool, error) {
	value := table.RawGetString(field)
	boolean, ok := value.(lua.LBool)
	if !ok {
		return false, fmt.Errorf("TTY event %s must be a boolean", field)
	}
	return bool(boolean), nil
}

func positiveInteger(table *lua.LTable, field string) (int, error) {
	value, ok := integerValue(table.RawGetString(field))
	if !ok || value < 1 || value > maxTerminalDimension {
		return 0, fmt.Errorf("TTY event %s must be an integer between 1 and %d", field, maxTerminalDimension)
	}
	return value, nil
}
