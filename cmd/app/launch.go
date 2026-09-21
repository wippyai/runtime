// SPDX-License-Identifier: MPL-2.0

package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/wippyai/runtime/api/boot"
)

// Op is the operation an invocation selects.
type Op int

const (
	OpRun Op = iota
	OpUpdate
	OpRecover
	OpWippy
)

// String returns the verb that selects the operation.
func (op Op) String() string {
	switch op {
	case OpUpdate:
		return "update"
	case OpRecover:
		return "recover"
	case OpWippy:
		return "wippy"
	default:
		return "run"
	}
}

// Launch describes one invocation to the host. State is already resolved and
// absolute, and Explicit reports that --state selected it. Owned answers
// whether a state has an owner, including a state the host selects itself.
type Launch struct {
	Command  string
	State    string
	Dir      string
	Args     []string
	Op       Op
	Explicit bool
}

// Plan is the host's decision for one launch. A non-empty DefaultState replaces
// the executable's default state when the invocation did not explicitly name
// one with --state. Command or Args replace the values the grammar selected.
// Run hands the whole launch to the host. Prepare opens host resources under
// the state lock and returns the configuration they need together with the
// close that releases them. Transient runs an ordinary application command in
// a private temporary state. It cannot accompany Run, and it is valid only for
// OpRun.
type Plan struct {
	Run          func(context.Context) error
	Prepare      func(context.Context) (boot.Config, func() error, error)
	DefaultState string
	Command      string
	Args         []string
	Transient    bool
}

// validate checks combinations a host can express that have no operation
// meaning. It runs before the runner opens either the selected or a temporary
// state directory.
func (p Plan) validate(l Launch) error {
	if !p.Transient {
		return nil
	}
	if l.Op != OpRun {
		return NewInvalidPlanError("transient execution is valid only for run")
	}
	if p.Run != nil {
		return NewInvalidPlanError("transient execution cannot accompany Run")
	}
	return nil
}

// Host decides what an invocation does before the runner opens the state.
type Host interface {
	Plan(ctx context.Context, l Launch) (Plan, error)
}

// parseLaunch reads the argument grammar. A leading --state is the only
// argument the runner consumes, so every other argument, including one shaped
// like a flag, reaches the verb and the application exactly as it was typed.
func parseLaunch(e Executable, args []string) (Launch, error) {
	state, remaining, err := parseState(args)
	if err != nil {
		return Launch{}, err
	}
	launch := Launch{Command: e.Command, Explicit: state != ""}
	if state == "" {
		config, err := os.UserConfigDir()
		if err != nil {
			return Launch{}, NewApplicationStateError("resolve default state directory", e.Name, err)
		}
		state = filepath.Join(config, e.Name)
	}
	absolute, err := filepath.Abs(state)
	if err != nil {
		return Launch{}, NewApplicationStateError("resolve state directory", state, err)
	}
	launch.State = absolute
	if launch.Dir, err = os.Getwd(); err != nil {
		return Launch{}, NewApplicationStateError("resolve working directory", "", err)
	}
	launch.Op, launch.Args = OpRun, remaining
	if len(remaining) > 0 {
		switch remaining[0] {
		case OpRun.String(), OpUpdate.String(), OpRecover.String(), OpWippy.String():
			launch.Op, launch.Args = verb(remaining[0]), remaining[1:]
		}
	}
	launch.Args = append([]string{}, launch.Args...)
	return launch, nil
}

// parseState takes a leading --state DIR or --state=DIR off the arguments and
// returns the directory it names with the arguments that follow it.
func parseState(args []string) (string, []string, error) {
	if len(args) == 0 {
		return "", args, nil
	}
	if value, joined := strings.CutPrefix(args[0], "--state="); joined {
		if value == "" {
			return "", nil, NewMissingStateDirectoryError()
		}
		return value, args[1:], nil
	}
	if args[0] == "--state" {
		if len(args) == 1 {
			return "", nil, NewMissingStateDirectoryError()
		}
		return args[1], args[2:], nil
	}
	return "", args, nil
}

// resolveDefaultState normalizes a host-selected default against the working
// directory captured before planning. A host may change the process working
// directory while it prepares its plan; that must not change where a relative
// default points.
func resolveDefaultState(dir, state string) (string, error) {
	if strings.ContainsRune(state, 0) {
		return "", errors.New("state directory contains NUL")
	}
	if !filepath.IsAbs(state) {
		state = filepath.Join(dir, state)
	}
	return filepath.Abs(state)
}

func verb(word string) Op {
	switch word {
	case OpUpdate.String():
		return OpUpdate
	case OpRecover.String():
		return OpRecover
	case OpWippy.String():
		return OpWippy
	default:
		return OpRun
	}
}

// Owned reports whether an invocation holds the state lock at the moment of
// the call. It is a snapshot: it opens the lock file only when that file
// already exists, releases the lock immediately and creates nothing, so it
// leaves an absent state absent. A host uses it to describe the state it is
// about to select; ErrOwned from Run is the authoritative answer.
func Owned(state string) (bool, error) {
	path := filepath.Join(state, lockFilename)
	file, err := os.OpenFile(path, os.O_RDWR, 0o600)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, NewApplicationStateError("probe application state lock", path, err)
	}
	defer func() { _ = file.Close() }()
	unlock, err := tryLockFile(file)
	if errors.Is(err, errLockBusy) {
		return true, nil
	}
	if err != nil {
		return false, NewApplicationStateError("probe application state lock", path, err)
	}
	if err := unlock(); err != nil {
		return false, NewApplicationStateError("release application state lock probe", path, err)
	}
	return false, nil
}
