// SPDX-License-Identifier: MPL-2.0

package exec

import (
	"fmt"
	"math"

	lua "github.com/wippyai/go-lua"
	execapi "github.com/wippyai/runtime/api/service/exec"
)

func parseConfinementPatch(table *lua.LTable) (*execapi.ConfinementPatch, error) {
	if err := requireConfineFields(table, "confine", "fs", "env", "network", "limits", "tree"); err != nil {
		return nil, err
	}
	patch := &execapi.ConfinementPatch{}
	var err error
	if value := table.RawGetString("fs"); value != lua.LNil {
		patch.FS, err = parseConfinementFS(value)
		if err != nil {
			return nil, err
		}
	}
	if value := table.RawGetString("env"); value != lua.LNil {
		patch.Env, err = parseConfinementEnv(value)
		if err != nil {
			return nil, err
		}
	}
	if value := table.RawGetString("network"); value != lua.LNil {
		patch.Network, err = parseConfinementNetwork(value)
		if err != nil {
			return nil, err
		}
	}
	if value := table.RawGetString("limits"); value != lua.LNil {
		patch.Limits, err = parseConfinementLimits(value)
		if err != nil {
			return nil, err
		}
	}
	if value := table.RawGetString("tree"); value != lua.LNil {
		patch.Tree, err = parseConfinementTree(value)
		if err != nil {
			return nil, err
		}
	}
	return patch, nil
}

func parseConfinementFS(value lua.LValue) (*execapi.ConfinementFSPatch, error) {
	table, ok := value.(*lua.LTable)
	if !ok {
		return nil, fmt.Errorf("confine.fs must be a table")
	}
	if err := requireConfineFields(table, "confine.fs", "read", "write", "exec"); err != nil {
		return nil, err
	}
	patch := &execapi.ConfinementFSPatch{}
	for _, field := range []struct {
		dest **[]string
		name string
	}{
		{&patch.Read, "read"},
		{&patch.Write, "write"},
		{&patch.Exec, "exec"},
	} {
		if value := table.RawGetString(field.name); value != lua.LNil {
			paths, err := parseConfinementStrings(value, "confine.fs."+field.name)
			if err != nil {
				return nil, err
			}
			*field.dest = &paths
		}
	}
	return patch, nil
}

func parseConfinementEnv(value lua.LValue) (*execapi.ConfinementEnvironmentPatch, error) {
	table, ok := value.(*lua.LTable)
	if !ok {
		return nil, fmt.Errorf("confine.env must be a table")
	}
	if err := requireConfineFields(table, "confine.env", "allow"); err != nil {
		return nil, err
	}
	patch := &execapi.ConfinementEnvironmentPatch{}
	if value := table.RawGetString("allow"); value != lua.LNil {
		allow, err := parseConfinementStrings(value, "confine.env.allow")
		if err != nil {
			return nil, err
		}
		patch.Allow = &allow
	}
	return patch, nil
}

func parseConfinementNetwork(value lua.LValue) (*string, error) {
	network, ok := value.(lua.LString)
	if !ok || string(network) != "none" {
		return nil, fmt.Errorf("confine.network must be none")
	}
	mode := string(network)
	return &mode, nil
}

func parseConfinementLimits(value lua.LValue) (*execapi.ConfinementLimitsPatch, error) {
	table, ok := value.(*lua.LTable)
	if !ok {
		return nil, fmt.Errorf("confine.limits must be a table")
	}
	if err := requireConfineFields(table, "confine.limits", "mem_mb", "pids", "wall_s"); err != nil {
		return nil, err
	}
	patch := &execapi.ConfinementLimitsPatch{}
	for _, field := range []struct {
		dest **int64
		name string
	}{
		{&patch.MemoryMiB, "mem_mb"},
		{&patch.PIDs, "pids"},
		{&patch.WallSec, "wall_s"},
	} {
		if value := table.RawGetString(field.name); value != lua.LNil {
			limit, err := parsePositiveConfinementInt(value, "confine.limits."+field.name)
			if err != nil {
				return nil, err
			}
			*field.dest = &limit
		}
	}
	return patch, nil
}

func parseConfinementTree(value lua.LValue) (*execapi.ConfinementTreePatch, error) {
	table, ok := value.(*lua.LTable)
	if !ok {
		return nil, fmt.Errorf("confine.tree must be a table")
	}
	if err := requireConfineFields(table, "confine.tree", "kill_on_owner_exit"); err != nil {
		return nil, err
	}
	patch := &execapi.ConfinementTreePatch{}
	if value := table.RawGetString("kill_on_owner_exit"); value != lua.LNil {
		enabled, ok := value.(lua.LBool)
		if !ok {
			return nil, fmt.Errorf("confine.tree.kill_on_owner_exit must be a boolean")
		}
		requested := bool(enabled)
		patch.KillOnOwnerExit = &requested
	}
	return patch, nil
}

func requireConfineFields(table *lua.LTable, prefix string, allowed ...string) error {
	var fieldErr error
	table.ForEach(func(key, _ lua.LValue) {
		if fieldErr != nil {
			return
		}
		name, ok := key.(lua.LString)
		if !ok {
			fieldErr = fmt.Errorf("%s keys must be strings", prefix)
			return
		}
		for _, field := range allowed {
			if string(name) == field {
				return
			}
		}
		fieldErr = fmt.Errorf("%s.%s is not a launch-time confinement option", prefix, name)
	})
	return fieldErr
}

func parseConfinementStrings(value lua.LValue, field string) ([]string, error) {
	table, ok := value.(*lua.LTable)
	if !ok {
		return nil, fmt.Errorf("%s must be an array of strings", field)
	}
	count := table.Len()
	values := make([]string, 0, count)
	for index := 1; index <= count; index++ {
		item, ok := table.RawGetInt(index).(lua.LString)
		if !ok || item == "" {
			return nil, fmt.Errorf("%s must be a contiguous array of nonempty strings", field)
		}
		values = append(values, string(item))
	}
	entries := 0
	table.ForEach(func(_, _ lua.LValue) { entries++ })
	if entries != count {
		return nil, fmt.Errorf("%s must be a contiguous array of strings", field)
	}
	return values, nil
}

func parsePositiveConfinementInt(value lua.LValue, field string) (int64, error) {
	switch value := value.(type) {
	case lua.LInteger:
		if value > 0 {
			return int64(value), nil
		}
	case lua.LNumber:
		number := float64(value)
		if number > 0 && number < math.Exp2(63) && math.Trunc(number) == number {
			return int64(number), nil
		}
	}
	return 0, fmt.Errorf("%s must be a positive integer", field)
}
