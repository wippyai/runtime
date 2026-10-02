// SPDX-License-Identifier: MPL-2.0

package eval

import (
	"errors"
	"fmt"
	"strings"

	lua "github.com/wippyai/go-lua"
	apihost "github.com/wippyai/runtime/api/host"
)

type linkModeDecl struct {
	name string
	mode apihost.EvalLinkMode
	seen bool
}

func (d *linkModeDecl) set(name string, mode apihost.EvalLinkMode) error {
	if d.seen {
		if d.mode != mode {
			return fmt.Errorf("eval %s conflicts with %s", name, d.name)
		}
		return nil
	}
	d.name, d.mode, d.seen = name, mode, true
	return nil
}

// parseLinkMode reads detached, monitor_only and link. Declarations that name
// different modes conflict.
func parseLinkMode(opts *lua.LTable) (apihost.EvalLinkMode, error) {
	var decl linkModeDecl
	if detached, ok, err := boolValue(opts.RawGetString(evalOptionDetached), evalOptionDetached); err != nil {
		return apihost.EvalLinkRequired, err
	} else if ok && detached {
		if err := decl.set(evalOptionDetached, apihost.EvalLinkDetached); err != nil {
			return apihost.EvalLinkRequired, err
		}
	}
	if mon, ok, err := boolValue(opts.RawGetString(evalOptionMonitorOnly), evalOptionMonitorOnly); err != nil {
		return apihost.EvalLinkRequired, err
	} else if ok && mon {
		if err := decl.set(evalOptionMonitorOnly, apihost.EvalLinkMonitorOnly); err != nil {
			return apihost.EvalLinkRequired, err
		}
	}
	if raw := opts.RawGetString(evalOptionLink); raw != lua.LNil {
		s, ok := raw.(lua.LString)
		if !ok {
			return apihost.EvalLinkRequired, errors.New("eval link must be string")
		}
		mode, err := linkModeFromString(string(s))
		if err != nil {
			return apihost.EvalLinkRequired, err
		}
		if err := decl.set(evalOptionLink, mode); err != nil {
			return apihost.EvalLinkRequired, err
		}
	}
	if !decl.seen {
		return apihost.EvalLinkRequired, nil
	}
	return decl.mode, nil
}

func linkModeFromString(raw string) (apihost.EvalLinkMode, error) {
	switch strings.ToLower(raw) {
	case "", "required":
		return apihost.EvalLinkRequired, nil
	case "detached":
		return apihost.EvalLinkDetached, nil
	case "monitor", "monitor_only", "monitor-only":
		return apihost.EvalLinkMonitorOnly, nil
	default:
		return apihost.EvalLinkRequired, fmt.Errorf("unknown eval link mode %q", raw)
	}
}
