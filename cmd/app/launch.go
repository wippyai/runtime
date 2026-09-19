// SPDX-License-Identifier: MPL-2.0

package app

import (
	"context"
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"

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
// absolute. Owned is a snapshot of the state lock taken before anything opens
// the state, so it reports the owner that existed at that moment.
type Launch struct {
	Command  string
	State    string
	Dir      string
	Args     []string
	Op       Op
	Explicit bool
	Owned    bool
}

// Plan is the host's decision for one launch. A non-empty State, Command or
// Args replaces the value the grammar selected. Run hands the whole launch to
// the host. Prepare opens host resources under the state lock and returns the
// configuration they need together with the close that releases them.
type Plan struct {
	Run     func(context.Context) error
	Prepare func(context.Context) (boot.Config, func() error, error)
	State   string
	Command string
	Args    []string
}

// Host decides what an invocation does before the runner opens the state.
type Host interface {
	Plan(ctx context.Context, l Launch) (Plan, error)
}

// parseLaunch reads the argument grammar. Parsing stops at the first argument
// that is not a host flag, so the verb and everything after it stay intact.
func parseLaunch(e Executable, args []string) (Launch, error) {
	flags := flag.NewFlagSet(e.Name, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	selected := flags.String("state", "", "application state directory")
	if err := flags.Parse(args); err != nil {
		return Launch{}, err
	}
	launch := Launch{Command: e.Command, Explicit: *selected != ""}
	state := *selected
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
	remaining := flags.Args()
	launch.Op, launch.Args = OpRun, remaining
	if len(remaining) > 0 {
		switch remaining[0] {
		case OpRun.String(), OpUpdate.String(), OpRecover.String(), OpWippy.String():
			launch.Op, launch.Args = verb(remaining[0]), remaining[1:]
		}
	}
	launch.Args = append([]string{}, launch.Args...)
	if launch.Owned, err = probeOwned(launch.State); err != nil {
		return Launch{}, err
	}
	return launch, nil
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

// probeOwned reports whether another invocation holds the state lock at the
// moment of the call. It opens the lock file only when it already exists and
// releases the lock immediately, so the probe leaves an absent state absent.
func probeOwned(state string) (bool, error) {
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
