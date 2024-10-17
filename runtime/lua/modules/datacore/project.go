package datacore

import (
	"context"
	"fmt"

	projectReq "git.spiralscout.com/estimation-engine/api/gen/go/core/request/project/v1"
	"git.spiralscout.com/estimation-engine/go-lua"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"go.uber.org/zap"
	"google.golang.org/grpc/metadata"
)

func (m *Module) getProjectSrv(l *lua.LState) int {
	m.log.Debug("called project service")

	// we expect only 1 arg - table with keys: folder_uuid, path, depth, type, metadata
	// path is required
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

	err := validate(reqt, "type")
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	// FORM THE REQUEST -----------------------------------------
	req := &projectReq.ProjectRequest{
		Type: engine.TableToSlice(reqt.RawGet(lua.LString("type")), m.log),
	}
	// ----------------------------------------------------------

	if reqt.RawGet(lua.LString("folder_uuid")) != lua.LNil {
		req.ProjectUuid = toPtr(reqt.RawGet(lua.LString("folder_uuid")).String())
	}

	if reqt.RawGet(lua.LString("path")) != lua.LNil {
		req.Path = toPtr(reqt.RawGet(lua.LString("path")).String())
	}

	if reqt.RawGet(lua.LString("depth")) != lua.LNil {
		val := reqt.RawGet(lua.LString("depth"))
		if num, ok := val.(lua.LNumber); ok {
			req.Depth = toPtr(int32(num))
		}
	}

	if reqt.RawGet(lua.LString("metadata")) != lua.LNil {
		req.Metadata = engine.TableToMap(engine.ToTable(reqt.RawGet(lua.LString("metadata"))), m.log)
	}

	resp, err := m.projectSrv.Project(metadata.NewOutgoingContext(context.Background(), metadata.Pairs("token", m.token)), req)
	if err != nil {
		m.log.Error("failed to query folders", zap.Error(err))
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
	pj := resp.GetProjects()

	for _, p := range pj {
		tmpt := l.NewTable()
		tmpt.RawSetString("description", lua.LString(p.GetDescription()))
		tmpt.RawSetString("path", lua.LString(p.GetPath()))
		tmpt.RawSetString("folder_uuid", lua.LString(p.GetProjectUuid()))
		tmpt.RawSetString("title", lua.LString(p.GetTitle()))
		tmpt.RawSetString("type", lua.LString(p.GetType()))
		tmpt.RawSetString("timestamp", lua.LString(p.GetTimestamp().String()))

		md := p.GetMetadata()
		if len(md) > 0 {
			tmtmd := l.NewTable()
			for _, v := range md {
				tmtmd.RawSetString("metadata_uuid", lua.LString(v.GetMetadataUuid()))
				tmtmd.RawSetString("timestamp", lua.LString(v.GetTimestamp().String()))
				tmtmd.RawSetString("version", lua.LNumber(v.GetVersion()))
				tmtmd.RawSetString("type", lua.LString(v.GetType()))
				tmtmd.RawSetString("content", lua.LString(v.GetContent()))
				tmtmd.RawSetString("discriminator", lua.LString(v.GetDiscriminator()))
			}

			tmpt.Append(tmpt)
		}

		t.Append(tmpt)
	}

	l.Push(t)
	l.Push(lua.LNil)

	return 2
}
