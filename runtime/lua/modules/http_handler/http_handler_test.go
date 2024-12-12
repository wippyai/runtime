package httphandler

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/ponyruntime/pony/runtime/lua/engine"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type TestStruct struct {
	t      *testing.T
	engn   *engine.Engine
	httphm *Module
}

func TestInit(t *testing.T) {
	l, _ := zap.NewDevelopment()
	eng := engine.NewLuaEngine(context.Background(), l)
	httpmod := New(l)
	tt := &TestStruct{
		t,
		eng,
		httpmod,
	}

	httpserv := &http.Server{
		Addr:    ":9999",
		Handler: tt,
	}

	eng.L.PreloadModule("http_handler", httpmod.Loader)

	go func() {
		httpserv.ListenAndServe()
	}()

	time.Sleep(time.Second)

	resp, err := http.Get("http://localhost:9999")
	require.NoError(t, err)
	require.Equal(t, resp.StatusCode, http.StatusOK)

	time.Sleep(time.Second * 4)
	httpserv.Close()
}

// can be done via callback actually, but...
func (tt *TestStruct) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	tt.httphm.Init(r, rw)

	luaCode := `
	local httph = require("http_handler")
	local h = httph.new()
	local method = h:method()
	return method
	`

	err := tt.engn.DoString(luaCode, "")
	require.NoError(tt.t, err)

	v := tt.engn.Get(-1)
	require.Equal(tt.t, "GET", v.String())

	return
}
