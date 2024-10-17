package datacore

import (
	"context"
	"fmt"

	dataReq "git.spiralscout.com/estimation-engine/api/gen/go/core/request/data/v1"
	"git.spiralscout.com/estimation-engine/go-lua"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"go.uber.org/zap"
	"google.golang.org/grpc/metadata"
)

func (m *Module) getDataSrv(l *lua.LState) int {
	m.log.Debug("called branch service")

	// we expect only 1 arg - table with keys: file_uuid, node_uuid, data_uuid, type, value, discriminator
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

	err := validate(reqt, "file_uuid", "node_uuid")
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	// FORM THE REQUEST -----------------------------------------
	req := &dataReq.DataRequest{
		BranchUuid: reqt.RawGet(lua.LString("file_uuid")).String(),
		NodeUuid:   toPtr(reqt.RawGet(lua.LString("node_uuid")).String()),
	}
	// ----------------------------------------------------------

	if reqt.RawGet(lua.LString("data_uuid")) != lua.LNil {
		req.DataUuid = toPtr(reqt.RawGet(lua.LString("data_uuid")).String())
	}

	if reqt.RawGet(lua.LString("type")) != lua.LNil {
		req.Type = toPtr(reqt.RawGet(lua.LString("type")).String())
	}

	if reqt.RawGet(lua.LString("value")) != lua.LNil {
		req.Value = toPtr(reqt.RawGet(lua.LString("value")).String())
	}

	if reqt.RawGet(lua.LString("discriminator")) != lua.LNil {
		req.Discriminator = toPtr(reqt.RawGet(lua.LString("discriminator")).String())
	}

	resp, err := m.dataSrv.Data(metadata.NewOutgoingContext(context.Background(), metadata.Pairs("token", m.token)), req)
	if err != nil {
		m.log.Error("failed to query data", zap.Error(err))
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
	dt := resp.Data

	for _, d := range dt {
		datat := l.NewTable()

		t.RawSetString("file_uuid", lua.LString(d.GetBranchUuid()))
		t.RawSetString("node_uuid", lua.LString(d.GetNodeUuid()))
		t.RawSetString("data_uuid", lua.LString(d.GetDataUuid()))
		t.RawSetString("timestamp", lua.LString(d.GetTimestamp().String()))
		t.RawSetString("version", lua.LNumber(d.GetVersion()))
		t.RawSetString("type", lua.LString(d.GetType()))
		t.RawSetString("content", lua.LString(d.GetContent()))
		t.RawSetString("value", lua.LNumber(d.GetValue()))
		t.RawSetString("discriminator", lua.LString(d.GetDiscriminator()))

		t.Append(datat)
	}

	l.Push(t)
	l.Push(lua.LNil)

	return 2
}
