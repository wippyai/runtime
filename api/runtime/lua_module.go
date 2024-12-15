package api

import "github.com/ponyruntime/go-lua"

type Module interface {
	Loader(*lua.LState) int
	Name() string
}
