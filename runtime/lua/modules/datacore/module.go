package datacore

import (
	"fmt"

	datacoreV1 "git.spiralscout.com/estimation-engine/api/gen/go/core/service/read/v1"
	"git.spiralscout.com/estimation-engine/go-lua"
	"go.uber.org/zap"
	"google.golang.org/grpc"
)

type Module struct {
	log        *zap.Logger
	token      string
	projectSrv datacoreV1.GetProjectServiceClient
	branchSrv  datacoreV1.GetBranchServiceClient
	nodeSrv    datacoreV1.GetNodeServiceClient
	dataSrv    datacoreV1.GetDataServiceClient
}

func NewModule(log *zap.Logger, gclient *grpc.ClientConn, token string) *Module {
	return &Module{
		token:      token,
		log:        log,
		projectSrv: datacoreV1.NewGetProjectServiceClient(gclient),
		branchSrv:  datacoreV1.NewGetBranchServiceClient(gclient),
		nodeSrv:    datacoreV1.NewGetNodeServiceClient(gclient),
		dataSrv:    datacoreV1.NewGetDataServiceClient(gclient),
	}
}

func toPtr[T any](v T) *T {
	return &v
}

func validate(t *lua.LTable, vals ...string) error {
	if vals == nil {
		return nil
	}

	for _, val := range vals {
		if t.RawGet(lua.LString(val)) == lua.LNil {
			return fmt.Errorf("%s is required", val)
		}
	}

	return nil
}
