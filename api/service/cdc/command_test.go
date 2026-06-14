// SPDX-License-Identifier: MPL-2.0

package cdc

import (
	"testing"

	"github.com/wippyai/runtime/api/dispatcher"
)

func TestCommandIDs(t *testing.T) {
	if Subscribe != dispatcher.CommandID(172) {
		t.Fatalf("Subscribe = %d, want 172", Subscribe)
	}
}
