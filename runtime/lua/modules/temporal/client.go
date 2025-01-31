package temporal

import (
	"github.com/ponyruntime/pony/service/temporal/client"
	lua "github.com/yuin/gopher-lua"
	temporal "go.temporal.io/sdk/client"
	"go.uber.org/zap"
)

// Client wraps a temporal client for Lua access
type Client struct {
	client *client.Client
	log    *zap.Logger
}

func CheckClient(l *lua.LState) *Client {
	ud := l.CheckUserData(1)
	if v, ok := ud.Value.(*Client); ok {
		return v
	}
	l.ArgError(1, "temporal client expected")
	return nil
}

func CheckTemporalClient(l *lua.LState) temporal.Client {
	c := CheckClient(l)
	if c == nil {
		l.ArgError(1, "temporal client not initialized")
		return nil
	}

	tClient, err := c.client.GetClient()
	if err != nil {
		l.ArgError(1, err.Error())
		return nil
	}

	return tClient
}

// Client methods
func healthcheck(l *lua.LState) int {
	c := CheckTemporalClient(l)
	_, err := c.CheckHealth(l.Context(), &temporal.CheckHealthRequest{})
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	l.Push(lua.LBool(true))
	return 1
}

// Register client methods
func registerClient(l *lua.LState, mod *lua.LTable) {
	mt := l.NewTypeMetatable("Temporal.Client")
	l.SetField(mt, "__index", l.SetFuncs(l.NewTable(), map[string]lua.LGFunction{
		"healthcheck": healthcheck,
	}))
}
