// SPDX-License-Identifier: MPL-2.0

package workflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/process"
)

type testCommand struct {
	id dispatcher.CommandID
}

func (c testCommand) CmdID() dispatcher.CommandID { return c.id }

func TestCopyOutputYieldsSurvivesReset(t *testing.T) {
	var out process.StepOutput

	out.Yield(testCommand{id: 1}, 11)
	out.Yield(testCommand{id: 2}, 22)
	out.Yield(testCommand{id: 3}, 33)

	snapshot := copyOutputYields(&out)
	out.Reset()

	assert.Len(t, snapshot, 3)
	assert.Equal(t, uint64(11), snapshot[0].Tag)
	assert.Equal(t, uint64(22), snapshot[1].Tag)
	assert.Equal(t, uint64(33), snapshot[2].Tag)
	assert.NotNil(t, snapshot[0].Cmd)
	assert.NotNil(t, snapshot[1].Cmd)
	assert.NotNil(t, snapshot[2].Cmd)
}
