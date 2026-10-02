// SPDX-License-Identifier: MPL-2.0

package sqlite

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/system/registry/history/historytest"
	"go.uber.org/zap"
)

func TestConformance(t *testing.T) {
	historytest.Run(t, func(t *testing.T) historytest.History {
		history, err := NewSQLite(filepath.Join(t.TempDir(), "history.db"), zap.NewNop())
		require.NoError(t, err)
		t.Cleanup(func() { _ = history.Close() })
		return history
	})
}
