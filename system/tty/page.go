// SPDX-License-Identifier: MPL-2.0

package tty

import (
	"context"

	"github.com/wippyai/runtime/api/runtime"
	ttyapi "github.com/wippyai/runtime/api/tty"
	"github.com/wippyai/runtime/service/terminal"
)

func (v *viewport) SetPage(ctx context.Context, page *ttyapi.Page) error {
	owner, ok := runtime.GetFramePID(ctx)
	s := v.session
	if !ok || !samePID(owner, v.owner) || !samePID(owner, s.creator) {
		return ttyapi.ErrPermissionDenied
	}
	var next *ttyapi.Page
	if page != nil {
		validated, err := page.Canonical()
		if err != nil {
			return err
		}
		next = &validated
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || v.closed.Load() {
		return ttyapi.ErrViewportClosed
	}
	if (next == nil && s.page == nil) || (next != nil && s.page != nil && *next == *s.page) {
		return nil
	}
	s.page = next
	s.pageRenderer = nil
	s.resolvePageRows(nil, nil)
	s.revision++
	for _, watcher := range s.watches {
		publishLatest(watcher.ch, ttyapi.Update{Revision: s.revision})
	}
	return nil
}

// resolvePageRows runs under session.mu. Unchanged source rows reuse the
// previously resolved string; Snapshot never parses or recolors anything.
func (s *session) resolvePageRows(previousSource, previousRows []string) {
	if s.page == nil {
		s.rows = s.sourceRows
		return
	}
	if s.pageRenderer == nil || s.pageRenderer.Width() != s.width {
		s.pageRenderer = terminal.NewPageRenderer(s.width, *s.page)
		previousSource, previousRows = nil, nil
	}
	rows := make([]string, s.height)
	blank := ""
	for y := range rows {
		raw := ""
		if y < len(s.sourceRows) {
			raw = s.sourceRows[y]
		}
		if y < len(previousSource) && y < len(previousRows) && raw == previousSource[y] {
			rows[y] = previousRows[y]
		} else if raw == "" {
			if blank == "" {
				blank = s.pageRenderer.RenderRow("")
			}
			rows[y] = blank
		} else {
			rows[y] = s.pageRenderer.RenderRow(raw)
		}
	}
	s.rows = rows
}

func (s *surface) Page() (ttyapi.Page, bool) {
	s.session.mu.RLock()
	defer s.session.mu.RUnlock()
	if s.session.page == nil {
		return ttyapi.Page{}, false
	}
	return *s.session.page, true
}
