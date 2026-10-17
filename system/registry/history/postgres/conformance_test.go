// SPDX-License-Identifier: MPL-2.0

package postgres

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/system/registry/history/historytest"
	"go.uber.org/zap"
)

func TestConformance(t *testing.T) {
	dsn := os.Getenv("WIPPY_POSTGRES_HISTORY_TEST_DSN")
	if dsn == "" {
		t.Skip("WIPPY_POSTGRES_HISTORY_TEST_DSN is not set")
	}
	historytest.Run(t, func(t *testing.T) historytest.History {
		schema := fmt.Sprintf("conformance_%d", time.Now().UnixNano())
		history, err := NewPostgres(dsn, schema, zap.NewNop())
		require.NoError(t, err)
		t.Cleanup(func() {
			_, _ = history.db.ExecContext(context.Background(), "DROP SCHEMA "+schema+" CASCADE")
			_ = history.Close()
		})
		return history
	})
}
