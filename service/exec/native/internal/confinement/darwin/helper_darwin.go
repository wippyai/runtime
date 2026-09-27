// SPDX-License-Identifier: MPL-2.0

//go:build darwin && cgo

package darwin

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

const (
	PolicyFD  = 3
	StatusFD  = 4
	WorkDirFD = 5
)

type HelperPolicy struct {
	Profile string   `json:"profile"`
	Path    string   `json:"path"`
	Argv    []string `json:"argv"`
	Env     []string `json:"env"`
}

func RunHelper() error {
	policyFile := os.NewFile(PolicyFD, "confine-policy")
	status := os.NewFile(StatusFD, "confine-status")
	if policyFile == nil || status == nil {
		return errors.New("missing confinement control descriptors")
	}
	defer policyFile.Close()
	defer status.Close()
	var policy HelperPolicy
	decoder := json.NewDecoder(io.LimitReader(policyFile, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&policy); err != nil {
		return fmt.Errorf("decode policy: %w", err)
	}
	unix.CloseOnExec(PolicyFD)
	if policy.Path == "" || len(policy.Argv) == 0 || policy.Argv[0] == "" || policy.Profile == "" {
		return errors.New("incomplete confinement policy")
	}
	if err := unix.Fchdir(WorkDirFD); err != nil {
		return fmt.Errorf("enter pinned work directory: %w", err)
	}
	unix.CloseOnExec(StatusFD)
	unix.CloseOnExec(WorkDirFD)
	if err := applySeatbelt(policy.Profile); err != nil {
		return fmt.Errorf("install Seatbelt policy: %w", err)
	}
	if _, err := status.WriteString("READY\n"); err != nil {
		return err
	}
	if err := syscall.Exec(policy.Path, policy.Argv, policy.Env); err != nil {
		_, _ = status.WriteString("ERROR " + err.Error() + "\n")
		return fmt.Errorf("exec target: %w", err)
	}
	return nil
}
