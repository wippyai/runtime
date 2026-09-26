// SPDX-License-Identifier: MPL-2.0

package exec

import (
	"testing"

	"github.com/stretchr/testify/require"
	execapi "github.com/wippyai/runtime/api/service/exec"
)

func TestConfineOptionFailsClosedUntilEnforcementIsInstalled(t *testing.T) {
	l := setupState()
	defer l.Close()

	options := l.NewTable()
	options.RawSetString("confine", l.NewTable())
	_, err := parseProcessOptions(options)
	require.ErrorIs(t, err, execapi.ErrConfineUnsupported)
}
