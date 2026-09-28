// SPDX-License-Identifier: MPL-2.0

package memory

import (
	"testing"

	"github.com/wippyai/runtime/system/registry/history/historytest"
)

func TestConformance(t *testing.T) {
	historytest.Run(t, func(*testing.T) historytest.History { return New() })
}
