// SPDX-License-Identifier: MPL-2.0

package app

import (
	"encoding/json"
	"fmt"
	"os"
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

// recordRecovery announces the recovery on stderr and writes its receipt. The
// receipt shares the directory the fresh history lives in, so writing it opens
// that directory for the history the runtime starts next.
func recordRecovery(e Executable, l Launch, deployment string) error {
	history := recoveryHistoryPath(l.State)
	fmt.Fprintf(os.Stderr, "recover: deployment %s bundle %s history %s\n", deployment, e.Bundle.ID(), history)
	digest, err := toolchain.ExecutableSHA256()
	if err != nil {
		return err
	}
	data, err := json.Marshal(receipt{
		Time:       time.Now().UTC().Format(time.RFC3339),
		Bundle:     e.Bundle.ID(),
		Deployment: deployment,
		History:    history,
		Executable: digest,
	})
	if err != nil {
		return NewApplicationStateError("encode recovery receipt", receiptPath(l.State), err)
	}
	return writeRecord(receiptPath(l.State), data)
}
