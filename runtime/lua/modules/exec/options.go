// SPDX-License-Identifier: MPL-2.0

package exec

import (
	"fmt"
	"math"

	lua "github.com/wippyai/go-lua"
	apiexec "github.com/wippyai/runtime/api/service/exec"
)

func parseProcessOptions(value lua.LValue) (apiexec.ProcessOptions, error) {
	options := apiexec.ProcessOptions{}
	if value == lua.LNil {
		return options, nil
	}
	table, ok := value.(*lua.LTable)
	if !ok {
		return options, fmt.Errorf("options must be a table")
	}
	if value := table.RawGetString("work_dir"); value != lua.LNil {
		workDir, ok := value.(lua.LString)
		if !ok {
			return options, fmt.Errorf("work_dir must be a string")
		}
		options.WorkDir = string(workDir)
	}
	if value := table.RawGetString("env"); value != lua.LNil {
		env, ok := value.(*lua.LTable)
		if !ok {
			return options, fmt.Errorf("env must be a table")
		}
		options.Env = make(map[string]string)
		var envErr error
		env.ForEach(func(key, value lua.LValue) {
			if envErr != nil {
				return
			}
			name, nameOK := key.(lua.LString)
			entry, entryOK := value.(lua.LString)
			if !nameOK || !entryOK {
				envErr = fmt.Errorf("env keys and values must be strings")
				return
			}
			options.Env[string(name)] = string(entry)
		})
		if envErr != nil {
			return apiexec.ProcessOptions{}, envErr
		}
	}
	if value := table.RawGetString("process_group"); value != lua.LNil {
		enabled, ok := value.(lua.LBool)
		if !ok {
			return apiexec.ProcessOptions{}, fmt.Errorf("process_group must be a boolean")
		}
		group := bool(enabled)
		options.ProcessGroup = &group
	}
	if value := table.RawGetString("mounts"); value != lua.LNil {
		mounts, ok := value.(*lua.LTable)
		if !ok {
			return options, fmt.Errorf("mounts must be a table")
		}
		options.Mounts = make([]apiexec.Mount, 0, mounts.Len())
		var mountsErr error
		var nextIndex int64 = 1
		mounts.ForEach(func(key, value lua.LValue) {
			if mountsErr != nil {
				return
			}
			index, indexOK := mountIndex(key)
			if !indexOK || index != nextIndex {
				mountsErr = fmt.Errorf("mounts must be a contiguous array")
				return
			}
			nextIndex++
			mount, mountOK := value.(*lua.LTable)
			if !mountOK {
				mountsErr = fmt.Errorf("mounts entries must be tables")
				return
			}
			source, sourceOK := mount.RawGetString("source").(lua.LString)
			target, targetOK := mount.RawGetString("target").(lua.LString)
			if !sourceOK || !targetOK {
				mountsErr = fmt.Errorf("mount source and target must be strings")
				return
			}
			readOnly := false
			if value := mount.RawGetString("read_only"); value != lua.LNil {
				readOnlyValue, readOnlyOK := value.(lua.LBool)
				if !readOnlyOK {
					mountsErr = fmt.Errorf("mount read_only must be a boolean")
					return
				}
				readOnly = bool(readOnlyValue)
			}
			options.Mounts = append(options.Mounts, apiexec.Mount{Source: string(source), Target: string(target), ReadOnly: readOnly})
		})
		if mountsErr != nil {
			return apiexec.ProcessOptions{}, mountsErr
		}
		if err := apiexec.ValidateMounts(options.Mounts); err != nil {
			return apiexec.ProcessOptions{}, err
		}
	}
	if value := table.RawGetString("pty"); value != lua.LNil {
		pty, ok := value.(*lua.LTable)
		if !ok {
			return options, fmt.Errorf("pty must be a table")
		}
		var err error
		options.PTY, err = parsePTYOptions(pty)
		if err != nil {
			return apiexec.ProcessOptions{}, err
		}
	}
	return options, nil
}

func mountIndex(value lua.LValue) (int64, bool) {
	switch value := value.(type) {
	case lua.LInteger:
		return int64(value), true
	case lua.LNumber:
		number := float64(value)
		if math.Trunc(number) == number {
			return int64(number), true
		}
	}
	return 0, false
}

func parsePTYOptions(table *lua.LTable) (*apiexec.PTYOptions, error) {
	width, err := parsePTYDimension(table.RawGetString("width"), "pty.width")
	if err != nil {
		return nil, err
	}
	height, err := parsePTYDimension(table.RawGetString("height"), "pty.height")
	if err != nil {
		return nil, err
	}

	term := ""
	if value := table.RawGetString("term"); value != lua.LNil {
		stringValue, ok := value.(lua.LString)
		if !ok {
			return nil, fmt.Errorf("pty.term must be a string")
		}
		term = string(stringValue)
	}

	options := &apiexec.PTYOptions{Width: width, Height: height, Term: term}
	if _, _, err := options.Dimensions(); err != nil {
		return nil, fmt.Errorf("invalid PTY dimensions: %w", err)
	}
	return options, nil
}

func parsePTYDimension(value lua.LValue, name string) (int, error) {
	if value == lua.LNil {
		return 0, nil
	}

	var number float64
	switch value := value.(type) {
	case lua.LInteger:
		number = float64(value)
	case lua.LNumber:
		number = float64(value)
	default:
		return 0, fmt.Errorf("%s must be an integer", name)
	}
	if math.Trunc(number) != number || number < 0 || number > apiexec.MaxPTYDimension {
		return 0, fmt.Errorf("%s must be 0 or an integer between 1 and %d", name, apiexec.MaxPTYDimension)
	}
	return int(number), nil
}

func pushInvalidOption(l *lua.LState, message string) {
	l.Push(lua.LNil)
	l.Push(lua.NewLuaError(l, message).WithKind(lua.Invalid).WithRetryable(false))
}
