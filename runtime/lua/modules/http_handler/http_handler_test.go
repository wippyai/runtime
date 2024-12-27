package httphandler

import (
	"context"
	"net/http"
	"testing"
	"time"

	actx "github.com/ponyruntime/pony/api/context"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	lpool "github.com/ponyruntime/pony/runtime/lua/luapool"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

type TestStruct struct {
	t *testing.T
}

type TestStructPool struct {
	t *testing.T
	p *lpool.Pool
}

func TestInit(t *testing.T) {
	tt := &TestStruct{
		t,
	}

	httpserv := &http.Server{
		ReadHeaderTimeout: time.Minute,
		Addr:              ":9999",
		Handler:           tt,
	}

	go func() {
		_ = httpserv.ListenAndServe()
	}()

	time.Sleep(time.Second)

	resp, err := http.Get("http://localhost:9999")
	require.NoError(t, err)
	require.Equal(t, resp.StatusCode, http.StatusOK)

	time.Sleep(time.Second * 4)
	httpserv.Close()
	_ = resp.Body.Close()
}

func TestInitWithPool(t *testing.T) {
	luaCode := `
	function handle(args)
		local httph = require("http_handler")
		local h = httph.new()
		local method = h:method()
		return method
	end

	return handle
	`

	scripts := make(map[string]*lpool.Config)
	scripts["1"] = lpool.NewPoolCfg(2, luaCode, "handle")

	l, _ := zap.NewDevelopment()
	pl, err := lpool.NewLuaPool(l, scripts, lpool.WithModules(New(l)))
	require.NoError(t, err)

	tt := &TestStructPool{
		t,
		pl,
	}

	httpserv := &http.Server{
		ReadHeaderTimeout: time.Minute,
		Addr:              ":9998",
		Handler:           tt,
	}

	go func() {
		_ = httpserv.ListenAndServe()
	}()

	time.Sleep(time.Second)

	resp, err := http.Get("http://localhost:9998")
	require.NoError(t, err)
	require.Equal(t, resp.StatusCode, http.StatusOK)

	time.Sleep(time.Second * 4)
	httpserv.Close()
	_ = resp.Body.Close()
}

// can be done via callback actually, but...
func (tt *TestStruct) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	l, _ := zap.NewDevelopment()
	ctx := context.WithValue(r.Context(), actx.HttpHandlerKey, actx.NewHTTPContextCarrier(r, rw))

	httpmod := New(l)
	eng := engine.NewLuaEngine(ctx, l)
	eng.L.PreloadModule("http_handler", httpmod.Loader)

	luaCode := `
	local httph = require("http_handler")
	local h = httph.new()
	local method = h:method()
	return method
	`

	err := eng.DoString(luaCode, "http_handler")
	require.NoError(tt.t, err)

	v := eng.Get(-1)
	require.Equal(tt.t, "GET", v.String())
}

// can be done via callback actually, but...
func (tt *TestStructPool) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	ctx := context.WithValue(r.Context(), actx.HttpHandlerKey, actx.NewHTTPContextCarrier(r, rw))

	data := make(map[string]any)
	data["hello"] = "heeeeeeeeeeelllo"

	fut := tt.p.Queue(ctx, lpool.NewPoolTask("1", data))
	for res := range fut {
		require.Equal(tt.t, "GET", res)
		break
	}
}
