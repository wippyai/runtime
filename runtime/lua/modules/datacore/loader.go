package datacore

import "git.spiralscout.com/estimation-engine/go-lua"

func (m *Module) Loader(l *lua.LState) int {
	t := l.NewTable()

	lapi := map[string]lua.LGFunction{
		"folders": m.getProjectSrv,
		"files":   m.getBranchSrv,
		"nodes":   m.getNodeSrv,
		"data":    m.getDataSrv,
	}

	l.SetFuncs(t, lapi)
	l.Push(t)

	return 1
}
