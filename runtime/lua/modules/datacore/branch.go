package datacore

import (
	"context"
	"fmt"

	branchReq "git.spiralscout.com/estimation-engine/api/gen/go/core/request/branch/v1"
	"git.spiralscout.com/estimation-engine/go-lua"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"go.uber.org/zap"
	"google.golang.org/grpc/metadata"
)

func (m *Module) getBranchSrv(l *lua.LState) int {
	m.log.Debug("called branch service")

	// we expect only 1 arg - table with keys: file_uuid, folder_uuid, type, owner_uuid, metadata
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
	req := &branchReq.BranchRequest{
		BranchUuid: engine.TableToSlice(reqt.RawGet(lua.LString("file_uuid")), m.log),
	}
	// ----------------------------------------------------------

	if reqt.RawGet(lua.LString("metadata")) != lua.LNil {
		req.Metadata = engine.TableToMap(engine.ToTable(reqt.RawGet(lua.LString("metadata"))), m.log)
	}

	if reqt.RawGet(lua.LString("folder_uuid")) != lua.LNil {
		req.ProjectUuid = engine.TableToSlice(reqt.RawGet(lua.LString("folder_uuid")), m.log)
	}

	if reqt.RawGet(lua.LString("type")) != lua.LNil {
		req.Type = engine.TableToSlice(reqt.RawGet(lua.LString("type")), m.log)
	}

	if reqt.RawGet(lua.LString("owner_uuid")) != lua.LNil {
		req.OwnerUuid = reqt.RawGet(lua.LString("owner_uuid")).String()
	}

	resp, err := m.branchSrv.Branch(metadata.NewOutgoingContext(context.Background(), metadata.Pairs("token", m.token)), req)
	if err != nil {
		m.log.Error("failed to query files", zap.Error(err))
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
	br := resp.GetBranches()

	for _, p := range br {
		tmpt := l.NewTable()
		tmpt.RawSetString("folder_uuid", lua.LString(p.GetProjectUuid()))
		tmpt.RawSetString("file_uuid", lua.LString(p.GetBranchUuid()))
		tmpt.RawSetString("timestamp", lua.LString(p.GetTimestamp().String()))
		tmpt.RawSetString("title", lua.LString(p.GetTitle()))
		tmpt.RawSetString("type", lua.LString(p.GetType()))
		tmpt.RawSetString("description", lua.LString(p.GetDescription()))
		tmpt.RawSetString("version", lua.LNumber(p.GetVersion()))

		md := p.GetMetadata()
		if len(md) > 0 {
			for _, v := range md {
				tmtmd := l.NewTable()

				tmtmd.RawSetString("metadata_uuid", lua.LString(v.GetMetadataUuid()))
				tmtmd.RawSetString("timestamp", lua.LString(v.GetTimestamp().String()))
				tmtmd.RawSetString("version", lua.LNumber(v.GetVersion()))
				tmtmd.RawSetString("type", lua.LString(v.GetType()))
				tmtmd.RawSetString("content", lua.LString(v.GetContent()))
				tmtmd.RawSetString("discriminator", lua.LString(v.GetDiscriminator()))

				tmpt.RawSetString("metadata", tmtmd)
			}
		}

		t.Append(tmpt)
	}

	l.Push(t)
	l.Push(lua.LNil)

	return 2
}
