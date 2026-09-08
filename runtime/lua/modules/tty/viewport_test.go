// SPDX-License-Identifier: MPL-2.0

package tty

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	lua "github.com/wippyai/go-lua"
	ctxapi "github.com/wippyai/runtime/api/context"
	ttyapi "github.com/wippyai/runtime/api/tty"
)

type viewportTestService struct {
	created  *viewportTestView
	attached *viewportTestView
	handle   string
}

func (s *viewportTestService) Create(context.Context, int, int) (ttyapi.Viewport, error) {
	return s.created, nil
}

func (s *viewportTestService) Attach(_ context.Context, handle string) (ttyapi.Viewport, error) {
	s.handle = handle
	return s.attached, nil
}

func (*viewportTestService) Binding(string) (ttyapi.Binding, error) { return nil, nil }
func (*viewportTestService) Close() error                           { return nil }

type viewportTestView struct {
	grant    string
	handle   string
	sent     []ttyapi.Event
	closeErr error
	snapshot ttyapi.Snapshot
	closed   bool
}

func (v *viewportTestView) Grant() string               { return v.grant }
func (v *viewportTestView) Handle() string              { return v.handle }
func (v *viewportTestView) Snapshot() ttyapi.Snapshot   { return v.snapshot }
func (*viewportTestView) Updates() <-chan ttyapi.Update { return nil }
func (v *viewportTestView) Send(event ttyapi.Event) error {
	v.sent = append(v.sent, event)
	return nil
}
func (*viewportTestView) Resize(int, int) error { return nil }
func (v *viewportTestView) Close() error        { v.closed = true; return v.closeErr }

func TestLuaViewportAttachHandleAndRevisionPolling(t *testing.T) {
	created := &viewportTestView{
		grant: "producer", handle: "viewer",
		snapshot: ttyapi.Snapshot{Revision: 7, Width: 40, Height: 12, Rows: []string{"ready"}},
	}
	attached := &viewportTestView{
		handle:   "viewer",
		snapshot: ttyapi.Snapshot{Revision: 7, Width: 40, Height: 12, Rows: []string{"ready"}},
	}
	service := &viewportTestService{created: created, attached: attached}
	ctx := ttyapi.WithService(ctxapi.NewRootContext(), service)

	l := lua.NewState()
	defer l.Close()
	bindTTY(l)
	l.SetContext(ctx)
	require.NoError(t, l.DoString(`
		local creator, create_err = tty.viewport({width = 40, height = 12})
		assert(creator and not create_err)
		local grant, grant_err = creator:grant()
		assert(grant == "producer" and not grant_err)
		assert(creator:handle() == "viewer")
		assert(creator:send({type = "visibility", visible = true}))
		assert(creator:send({type = "resize", width = 100, height = 30}))
		local first = creator:snapshot()
		assert(first.revision == 7 and first.rows[1] == "ready")
		assert(creator:snapshot(first.revision) == nil)

		local viewer, attach_err = tty.attach(creator:handle())
		assert(viewer and not attach_err)
		local attached_grant, attached_grant_err = viewer:grant()
		assert(attached_grant == nil and attached_grant_err)
		assert(viewer:handle() == "viewer")
		assert(viewer:close())
	`))
	require.Equal(t, "viewer", service.handle)
	require.Equal(t, []ttyapi.Event{
		{Type: "visibility", Visible: true},
		{Type: "resize", Width: 100, Height: 30},
	}, created.sent)
	require.True(t, attached.closed)
}

func TestLuaViewportSnapshotRowsRemainIndependent(t *testing.T) {
	view := &viewportTestView{snapshot: ttyapi.Snapshot{
		Revision: 1, Width: 10, Height: 2, Rows: []string{"first", "stable"},
	}}
	l := lua.NewState()
	defer l.Close()
	bindTTY(l)
	l.SetContext(ttyapi.WithService(ctxapi.NewRootContext(), &viewportTestService{created: view}))
	require.NoError(t, l.DoString(`
		view = assert(tty.viewport())
		old = view:snapshot()
		edited = view:snapshot()
		edited.rows[1] = "caller mutation"
		edited.rows[2] = nil
		edited.width = 99
		fresh = view:snapshot()
		assert(fresh.rows[1] == "first" and fresh.rows[2] == "stable")
		assert(fresh.width == 10)
	`))
	view.snapshot.Revision++
	view.snapshot.Rows = []string{"changed", "stable"}
	require.NoError(t, l.DoString(`
		new = view:snapshot(old.revision)
		assert(new.rows[1] == "changed" and new.rows[2] == "stable")
		assert(old.rows[1] == "first" and old.rows[2] == "stable")
	`))
	view.snapshot.Revision++
	view.snapshot.Height = 1
	view.snapshot.Rows = []string{"small"}
	require.NoError(t, l.DoString(`
		local small = view:snapshot()
		assert(#small.rows == 1 and small.rows[1] == "small")
		assert(new.rows[2] == "stable")
	`))
	wrapper := l.GetGlobal("view").(*lua.LUserData).Value.(*viewportWrapper)
	require.Len(t, wrapper.rows, 1)
	require.Equal(t, 1, cap(wrapper.rows))
	view.snapshot.Revision++
	view.snapshot.Rows = nil
	require.NoError(t, l.DoString(`assert(#view:snapshot().rows == 0)`))
	require.Empty(t, wrapper.rows)
	view.snapshot.Rows = []string{"after clear"}
	require.NoError(t, l.DoString(`
		assert(view:snapshot().rows[1] == "after clear")
		assert(view:close())
		assert(old.rows[1] == "first" and new.rows[1] == "changed")
	`))
	require.Nil(t, wrapper.rows)
}

func TestLuaViewportRejectsMalformedInput(t *testing.T) {
	created := &viewportTestView{}
	service := &viewportTestService{created: created}
	ctx := ttyapi.WithService(ctxapi.NewRootContext(), service)

	l := lua.NewState()
	defer l.Close()
	bindTTY(l)
	l.SetContext(ctx)
	require.NoError(t, l.DoString(`
		local invalid_dimension, dimension_err = tty.viewport({width = 1.5})
		assert(invalid_dimension == nil and dimension_err)
		local oversized, oversized_err = tty.viewport({width = 65535, height = 65535})
		assert(oversized == nil and oversized_err)

		local viewport = assert(tty.viewport())
		local sent, send_err = viewport:send({type = "resize", width = "80", height = 24})
		assert(sent == nil and send_err)
		sent, send_err = viewport:send({type = "visibility", visible = 1})
		assert(sent == nil and send_err)
		sent, send_err = viewport:send({type = "unknown"})
		assert(sent == nil and send_err)
		local resized, resize_err = viewport:resize(80.5, 24)
		assert(resized == nil and resize_err)
		resized, resize_err = viewport:resize(65535, 65535)
		assert(resized == nil and resize_err)
		assert(not pcall(function() viewport:snapshot(1.5) end))
	`))
	require.Empty(t, created.sent)
}

func TestLuaViewportClosePreservesFailure(t *testing.T) {
	created := &viewportTestView{closeErr: errors.New("close failed")}
	service := &viewportTestService{created: created}
	ctx := ttyapi.WithService(ctxapi.NewRootContext(), service)

	l := lua.NewState()
	defer l.Close()
	bindTTY(l)
	l.SetContext(ctx)
	require.NoError(t, l.DoString(`
		local viewport = assert(tty.viewport())
		for _ = 1, 2 do
			local closed, close_err = viewport:close()
			assert(closed == nil and tostring(close_err):find("close failed", 1, true))
		end
	`))
	require.True(t, created.closed)
}
