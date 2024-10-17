package env

import (
	"context"

	vaultReqV1 "git.spiralscout.com/estimation-engine/api/gen/go/vault/values/request/v1"
	vaultServiceV1 "git.spiralscout.com/estimation-engine/api/gen/go/vault/values/service/v1"
	"git.spiralscout.com/estimation-engine/go-lua"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
)

type Module struct {
	allowedList  map[string]string
	token        string
	logger       *zap.Logger
	vaultService vaultServiceV1.GetValueServiceClient
}

func NewEnvKeysModule(gclient *grpc.ClientConn, token string, allowedList map[string]string, lg *zap.Logger) *Module {
	vaultService := vaultServiceV1.NewGetValueServiceClient(gclient)
	return &Module{
		logger:       lg,
		vaultService: vaultService,
		token:        token,
		allowedList:  allowedList,
	}
}

func (m *Module) getKey(l *lua.LState) int {
	m.logger.Debug("env.key module called")

	if l.GetTop() != 1 {
		l.Push(lua.LNil)
		l.Push(lua.LString("expected 1 argument, vault key"))
		return 2
	}

	key := l.CheckString(1)
	if key == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("provided key from lua is empty"))
		return 2
	}

	val, ok := m.allowedList[key]
	if !ok {
		l.Push(lua.LNil)
		l.Push(lua.LString("provided key is not allowed"))
		return 2
	}

	resp, err := m.vaultService.Get(metadata.NewOutgoingContext(context.Background(), metadata.Pairs("token", m.token)), &vaultReqV1.GetRequest{
		Token: m.token,
		Key:   val,
	})

	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(err.Error()))
		return 2
	}

	value := resp.GetValue()
	if value == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("empty response from the vault"))
		return 2
	}

	// return keys
	l.Push(lua.LString(value))
	l.Push(lua.LNil)

	return 2
}
