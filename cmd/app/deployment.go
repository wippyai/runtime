// SPDX-License-Identifier: MPL-2.0

package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// current names the deployment the state runs. Base is the bundle identity of
// the executable that produced the directory, so an executable recognizes the
// deployments it may continue from the ones another executable left behind.
type current struct {
	Directory string `json:"directory"`
	Base      string `json:"base"`
	Previous  string `json:"previous,omitempty"`
}

// readCurrent returns the current record, or false when the state holds none.
func readCurrent(state string) (current, bool, error) {
	path := currentPath(state)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return current{}, false, nil
	}
	if err != nil {
		return current{}, false, NewApplicationStateError("read current deployment record", path, err)
	}
	var record current
	if err := json.Unmarshal(data, &record); err != nil {
		return current{}, false, NewCurrentDeploymentError("record is not readable", path, err)
	}
	if !filepath.IsLocal(record.Directory) || !strings.HasPrefix(filepath.ToSlash(record.Directory), deploymentsDir+"/") {
		return current{}, false, NewCurrentDeploymentError("directory is not a deployment of this state", record.Directory, nil)
	}
	return record, true, nil
}

// selectDeployment resolves the deployment an operation runs. Recovery always
// starts the shipped packs; every other operation continues the current
// deployment when this executable produced it, and otherwise starts from its
// own bundle. A current deployment another executable produced stays where it
// is: it is the history that executable can return to.
func selectDeployment(e Executable, l Launch) (string, error) {
	bundle := filepath.Join(deploymentsPath(l.State), e.Bundle.ID())
	if l.Op == OpRecover {
		return bundle, nil
	}
	record, found, err := readCurrent(l.State)
	if err != nil {
		return "", err
	}
	if !found {
		return bundle, nil
	}
	if record.Base != e.Bundle.ID() {
		fmt.Fprintf(os.Stderr, "%s: deployment %s is superseded by this executable, which runs bundle %s\n",
			e.Name, record.Directory, e.Bundle.ID())
		return bundle, nil
	}
	return filepath.Join(l.State, record.Directory), nil
}

// historyFor returns the registry history an operation boots. Recovery boots a
// history of its own, so the shipped packs start from the entries they carry
// while the deployment history stays in place.
func historyFor(l Launch) string {
	if l.Op == OpRecover {
		return recoveryHistoryPath(l.State)
	}
	return historyPath(l.State)
}
