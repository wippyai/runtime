package datacore

import (
	"context"
	"fmt"

	nodeReq "git.spiralscout.com/estimation-engine/api/gen/go/core/request/node/v1"
	nodeV1 "git.spiralscout.com/estimation-engine/api/gen/go/core/response/node/v1"
	"git.spiralscout.com/estimation-engine/go-lua"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"go.uber.org/zap"
	"google.golang.org/grpc/metadata"
)

func (m *Module) getNodeSrv(l *lua.LState) int {
	m.log.Debug("called node service")

	// we expect only 1 arg - table with keys: file_uuid, node_uuid, path, depth, sort_by_index, with_data, fetch_as_tree
	// file_uuid is required
	if l.GetTop() != 1 {
		l.Push(lua.LNil) // return nil
		l.Push(lua.LString("expected 1 argument"))
		return 2
	}

	luareq := l.Get(-1)
	// check if arg is table
	// if not return nil
	if luareq.Type() != lua.LTTable {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("expected table, got: %s", luareq.Type().String())))
		return 2
	}

	// get table
	reqt := engine.ToTable(luareq)
	if reqt == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("expected table, but got nil"))
		return 2
	}

	err := validate(reqt, "file_uuid")
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	// FORM THE REQUEST -----------------------------------------
	req := &nodeReq.NodeRequest{
		BranchUuid: reqt.RawGet(lua.LString("file_uuid")).String(),
	}
	// ----------------------------------------------------------

	if reqt.RawGet(lua.LString("node_uuid")) != lua.LNil {
		req.NodeUuid = engine.TableToSlice(reqt.RawGet(lua.LString("node_uuid")), m.log)
	}

	if reqt.RawGet(lua.LString("path")) != lua.LNil {
		req.Path = toPtr(reqt.RawGet(lua.LString("path")).String())
	}

	if reqt.RawGet(lua.LString("depth")) != lua.LNil {
		req.Depth = toPtr(reqt.RawGet(lua.LString("depth")).String())
	}

	if reqt.RawGet(lua.LString("sort_by_index")) != lua.LNil {
		d := reqt.RawGet(lua.LString("sort_by_index"))
		if sbi, ok := d.(lua.LBool); ok {
			req.SortByIndex = toPtr(bool(sbi))
		}
	}

	if reqt.RawGet(lua.LString("with_data")) != lua.LNil {
		d := reqt.RawGet(lua.LString("with_data"))
		if sbi, ok := d.(lua.LBool); ok {
			req.WithData = toPtr(bool(sbi))
		}
	}

	if reqt.RawGet(lua.LString("fetch_as_tree")) != lua.LNil {
		d := reqt.RawGet(lua.LString("fetch_as_tree"))
		if sbi, ok := d.(lua.LBool); ok {
			req.FetchAsTree = toPtr(bool(sbi))
		}
	}

	resp, err := m.nodeSrv.Node(metadata.NewOutgoingContext(context.Background(), metadata.Pairs("token", m.token)), req)
	if err != nil {
		m.log.Error("failed to query nodes", zap.Error(err))
		l.Push(lua.LNil)
		return 1
	}

	if resp == nil {
		m.log.Warn("no data returned")
		l.Push(lua.LNil)
		l.Push(lua.LNil)

		return 2
	}

	t := l.NewTable()
	nodes := resp.GetNodes()
	for _, p := range nodes {
		parseNodeHelper(p, l, t)
	}

	l.Push(t)
	l.Push(lua.LNil)

	return 2
}

func parseNodeHelper(node *nodeV1.Node, l *lua.LState, roottable *lua.LTable) {
	if node == nil {
		return
	}

	t := l.NewTable()

	t.RawSetString("file_uuid", lua.LString(node.GetBranchUuid()))
	t.RawSetString("node_uuid", lua.LString(node.GetNodeUuid()))
	t.RawSetString("timestamp", lua.LString(node.GetTimestamp().String()))
	t.RawSetString("type", lua.LString(node.GetType()))
	t.RawSetString("path", lua.LString(node.GetPath()))

	if len(node.GetData()) > 0 {
		dataUpper := l.NewTable()

		for _, v := range node.GetData() {
			datat := l.NewTable()

			datat.RawSetString("file_uuid", lua.LString(v.GetBranchUuid()))
			datat.RawSetString("node_uuid", lua.LString(v.GetNodeUuid()))
			datat.RawSetString("data_uuid", lua.LString(v.GetDataUuid()))
			datat.RawSetString("timestamp", lua.LString(v.GetTimestamp().String()))
			datat.RawSetString("version", lua.LNumber(v.GetVersion()))
			datat.RawSetString("type", lua.LString(v.GetType()))
			datat.RawSetString("content", lua.LString(v.GetContent()))
			datat.RawSetString("value", lua.LNumber(v.GetValue()))
			datat.RawSetString("discriminator", lua.LString(v.GetDiscriminator()))

			dataUpper.Append(datat)
		}

		t.RawSetString("data", dataUpper)
	}

	if len(node.GetNodes()) > 0 {
		nt := l.NewTable()
		for _, n := range node.GetNodes() {
			// here we go again, recursion
			parseNodeHelper(n, l, nt)
		}

		t.RawSetString("nodes", nt)
	}

	roottable.Append(t)
}
