package api

import "git.spiralscout.com/estimation-engine/go-lua"

type Module interface {
	Loader(*lua.LState) int
	Name() string
}
