// SPDX-License-Identifier: MPL-2.0

package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/wippyai/runtime/internal/toolchain"
)

// receipt records what a recovery booted, so the deployment it replaced can be
// identified from the state directory alone.
type receipt struct {
	Time       string `json:"time"`
	Bundle     string `json:"bundle"`
	Deployment string `json:"deployment"`
	History    string `json:"history"`
	Executable string `json:"executable_sha256"`
}

// recordRecovery gives every recovery a new history and points the latest
// receipt at it. Earlier recovery histories remain available for inspection.
func recordRecovery(e Executable, l Launch, deployment string) (history string, result error) {
	if err := os.MkdirAll(recoveryPath(l.State), 0o700); err != nil {
		return "", NewApplicationStateError("create recovery directory", recoveryPath(l.State), err)
	}
	run, err := os.MkdirTemp(recoveryPath(l.State), "run-")
	if err != nil {
		return "", NewApplicationStateError("create recovery history directory", recoveryPath(l.State), err)
	}
	defer func() {
		if result != nil {
			_ = os.Remove(run)
		}
	}()
	history = filepath.Join(run, historyFilename)
	digest, err := toolchain.ExecutableSHA256()
	if err != nil {
		return "", err
	}
	data, err := json.Marshal(receipt{
		Time:       time.Now().UTC().Format(time.RFC3339),
		Bundle:     e.Bundle.ID(),
		Deployment: deployment,
		History:    history,
		Executable: digest,
	})
	if err != nil {
		return "", NewApplicationStateError("encode recovery receipt", receiptPath(l.State), err)
	}
	if err := writeRecord(receiptPath(l.State), data); err != nil {
		return "", err
	}
	fmt.Fprintf(os.Stderr, "recover: deployment %s bundle %s history %s\n", deployment, e.Bundle.ID(), history)
	return history, nil
}
