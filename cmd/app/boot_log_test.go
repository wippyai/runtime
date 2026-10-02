// SPDX-License-Identifier: MPL-2.0

package app

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

type loggingHost struct {
	log *zap.Logger
	plannedHost
}

func (h *loggingHost) BootLogger() *zap.Logger { return h.log }

func TestBootPhaseUsesOptionalHostLogger(t *testing.T) {
	core, observed := observer.New(zap.InfoLevel)
	bootPhase(Executable{Host: &loggingHost{log: zap.New(core)}}, "lua_cache_seed", "end")
	entries := observed.All()
	require.Len(t, entries, 1)
	require.Equal(t, "Boot phase", entries[0].Message)
	require.Equal(t, map[string]any{"phase": "lua_cache_seed", "stage": "end"}, entries[0].ContextMap())
	bootPhase(Executable{}, "phase", "begin")
	bootPhase(Executable{Host: &loggingHost{}}, "phase", "begin")
	require.Equal(t, 1, observed.Len())
}
