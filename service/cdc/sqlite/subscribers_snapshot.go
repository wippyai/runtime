// SPDX-License-Identifier: MPL-2.0

//go:build sqlite_preupdate_hook

package sqlite

import (
	"context"
	"strings"

	config "github.com/wippyai/runtime/api/service/cdc"
)

func (s *subscription) isClosed() bool {
	return s.closed.Load()
}

func (s *subscription) finishSnapshot() {
	if s.snap != nil {
		close(s.snap)
	}
}

func (s *subscription) sendSnapshot(ctx context.Context, change config.Change) bool {
	select {
	case s.snap <- change:
		return true
	case <-s.done:
		return false
	case <-s.termCh:
		return false
	case <-ctx.Done():
		return false
	}
}

func (s *subscription) tableAllowed(name string) bool {
	if len(s.tables) == 0 {
		return true
	}
	_, ok := s.tables[strings.ToLower(name)]

	return ok
}
