package engine2

import (
	"testing"

	lua "github.com/yuin/gopher-lua"
)

func TestLocalForkVerify(t *testing.T) {
	t.Logf("LocalForkMarker: %s", lua.LocalForkMarker)
}
