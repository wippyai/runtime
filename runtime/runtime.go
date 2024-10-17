// Package runtime provides the runtime environment for the application.
// It knows about all underlying components and is responsible for their lifecycle.
package runtime

import (
	"context"
	"errors"
	"net/http"

	"github.com/ponyruntime/pony/api"
	eventsbus "github.com/ponyruntime/pony/eventbus"
	"github.com/ponyruntime/pony/futures"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	base64M "github.com/ponyruntime/pony/runtime/lua/modules/base64"
	httpM "github.com/ponyruntime/pony/runtime/lua/modules/http"
	sqlM "github.com/ponyruntime/pony/runtime/lua/modules/sql"
	"go.uber.org/zap"
)

// Runtime ... TODO: add all components field here
type Runtime struct {
	queue   *futures.Queue
	stop    chan struct{}
	log     *zap.Logger
	evBusID string
	eb      *eventsbus.Bus
}

func NewRuntime(log *zap.Logger, queue *futures.Queue) *Runtime {
	eb, id := eventsbus.NewEventBus()
	return &Runtime{
		queue:   queue,
		stop:    make(chan struct{}, 1),
		log:     log,
		evBusID: id,
		eb:      eb,
	}
}

func (r *Runtime) ListenEvents() {
	evCh := make(chan api.Event, 10)
	// can't be an error here since we're provided all the data
	_ = r.eb.SubscribeP(
		context.Background(),
		r.evBusID,
		api.SubSystemRuntime,
		api.EventsAll,
		evCh,
	)

	// listen for events
	go func() {
		for event := range evCh {
			switch event.SubSystem() {
			// broadcast events
			case api.SubSystemAll:
				switch event.Type() {
				case api.EventConfigurationUpdated:
					// handle configuration update
					r.log.Debug("received a configuration update event", zap.Any("content", event.Content()))
					// TODO enable subsystems according to the configuration, e.g.:
				}
			// listen only for the runtime events
			case api.SubSystemRuntime:
				// handle events
				switch event.Type() {
				case api.EventFatalError:
					r.log.Error("received a fatal error event", zap.Any("message", event.Content()))
					return
				default:
					r.log.Info("received an unknown event", zap.Any("type", event.Type()))
				}
			}
		}
	}()

	go func() {
		// start processing the queue
		r.Process()
	}()
}

func (r *Runtime) Process() {
	// TODO: we should be able to stop processing, it probably should be done in the Queue itself, it should close a channel on a broadcast stop event
	// BUT!!! we also need to track responses from all subsystems, that they're stopped
	for v := range r.queue.All() {
		// process and response
		if v.Registry == nil {
			select {
			case v.Response <- &api.TaskResult{
				ID:    v.ID,
				Error: errors.New("registry is nil"),
			}:
				r.log.Debug("runtime: sent error response", zap.String("id", v.ID))
			default:
				r.log.Warn("runtime: unable to send response", zap.String("id", v.ID))
			}
			continue
		}

		for _, target := range v.Registry.Apps {
			for i := 0; i < len(target.Modules); i++ {
				// modules to call
				switch target.Modules[i] {
				case "lua":
					e := engine.NewLuaEngine(context.Background(), r.log.Named("engine"))
					// Convert toolCall.Args to Lua table
					e.SetGlobal("args", engine.GoToLua(e.L, "args"))
					// Load modules ---------------------------
					e.L.Register("sql", sqlM.NewModule(r.log.Named("sql")).Loader)
					e.L.Register("http", httpM.NewHTTPModule(&http.Client{}, r.log.Named("http")).Loader)
					e.L.Register("base64", base64M.Loader)
					// -----------------------------------------

					//solRsp, err := dataServiceV1.NewSolutionServiceClient(a.eecoreclient).GetSolution(metadata.NewOutgoingContext(ctx, metadata.Pairs("token", string(token))), &solutionV1.GetSolutionRequest{
					//	Resource: tctx.Resource,
					//})
					//if err != nil {
					//	return nil, err
					//}

					//if solRsp.GetSolution() == nil || len(solRsp.GetSolution().GetPayloads()) == 0 {
					//	return nil, status.Error(codes.Internal, "no solution found")
					//}

					//payload := solRsp.GetSolution().GetPayloads()
					//luaCode := payload["application/x-lua"]
					//deps := payload["dependencies"]
					//

					//if deps != "" {
					//	// request dependencies
					//	/*
					//		"dependencies" => "{"libraries":{"random_jokes":"spw:018fb6ca3626704fbce429f4d45dd66b:lib:random_jokes:latest:e652fd205a2e"}}"
					//	*/
					//	a.log.Debug("unmarshalling data", zap.Any("data", deps))
					//	depsStruct := &api.Dependencies{}
					//	err = json.Unmarshal([]byte(deps), depsStruct)
					//	if err != nil {
					//		return &api.EdgeCallResult{
					//			ID:            tctx.ID,
					//			Ok:            false,
					//			ResultPayload: err.Error(),
					//		}, nil
					//	}
					//
					//	// preload only allowed extensions
					//	if len(depsStruct.Extensions) > 0 {
					//		// todo: encapsulate whole engine init away from here
					//		for k := range depsStruct.Extensions {
					//			switch k {
					//			case "sql":
					//				a.log.Debug("preloading sql extension")
					//				LE.L.PreloadModule("sql", cloudsql.NewModule(a.log.Named("sql")).Loader)
					//			case "datacore":
					//				a.log.Debug("preloading datacore extension")
					//				LE.L.PreloadModule(k, clouddc.NewModule(a.log.Named("datacore"), a.eecoreclient, string(token)).Loader)
					//			case "treesitter":
					//				a.log.Debug("preloading treesitter extension")
					//				LE.L.PreloadModule(k, cloudts.NewModule(a.log.Named("treesitter")).Loader)
					//			case "env":
					//				if len(depsStruct.Keys) == 0 {
					//					a.log.Warn("env extension is enabled but no keys provided, skipping")
					//					continue
					//				}
					//				a.log.Debug("preloading env extension")
					//				LE.L.PreloadModule("env", cloudkey.NewEnvKeysModule(a.usersclient, string(token), depsStruct.Keys, a.log.Named("env")).Loader)
					//			case "llm":
					//				a.log.Debug("preloading llm extension")
					//				LE.L.PreloadModule("llm", cloudllm.NewLLMModule(a.mlqclient, string(token), a.log.Named("llm")).Loader)
					//			case "ctx":
					//				a.log.Debug("preloading ctx extension")
					//				// virtual context module to track access to ctx dependency
					//				LE.L.PreloadModule("ctx", func(L *lua.LState) int {
					//					ctxData, err := dataServiceV1.NewGetDataServiceClient(a.eecoreclient).Data(
					//						metadata.NewOutgoingContext(ctx, metadata.Pairs("token", string(token))), &dataV1.DataRequest{
					//							BranchUuid: tctx.ContextAddr.BranchUUID,
					//							NodeUuid:   toPtr(tctx.ContextAddr.NodeUUID),
					//						})
					//					if err != nil {
					//						a.log.Error("failed to get context data", zap.Error(err))
					//						return 0
					//					}
					//
					//					contextValues := make(map[string]string)
					//					d := ctxData.GetData()
					//					for _, v := range d {
					//						contextValues[v.GetDiscriminator()] = v.GetContent()
					//					}
					//
					//					L.Push(lengine.GoToLua(L, contextValues))
					//					return 1
					//				})
					//			default:
					//				a.log.Debug("preloading extension", zap.String("name", k))
					//				err = LE.PreloadModule(api.Cloud, api.Extension(k))
					//				if err != nil {
					//					return &api.EdgeCallResult{
					//						ID:            tctx.ID,
					//						Ok:            false,
					//						ResultPayload: err.Error(),
					//					}, nil
					//				}
					//			}
					//		}
					//	}
					//
					//	if len(depsStruct.Libraries) > 0 {
					//		libsResources := make([]string, 0, len(depsStruct.Libraries))
					//		for _, v := range depsStruct.Libraries {
					//			libsResources = append(libsResources, v)
					//		}
					//
					//		// fetch libraries
					//		libsResp, err := dataServiceV1.NewSolutionServiceClient(a.eecoreclient).GetSolutions(metadata.NewOutgoingContext(ctx, metadata.Pairs("token", string(token))), &solutionV1.GetSolutionsRequest{
					//			Resources: libsResources,
					//		})
					//
					//		if err != nil {
					//			return &api.EdgeCallResult{
					//				ID:            tctx.ID,
					//				Ok:            false,
					//				ResultPayload: strings.Join([]string{err.Error(), LE.GetPrints()}, "\n"),
					//			}, nil
					//		}
					//
					//		if len(libsResp.GetSolutions()) == 0 || len(libsResp.GetSolutions()[0].GetPayloads()) == 0 {
					//			return nil, status.Error(codes.Internal, "no lua libraries found")
					//		}
					//		// here we need to use name and application/x-lua for every library we've got
					//
					//		for _, v := range libsResp.GetSolutions() {
					//			LE.PreloadLibrary(v.GetName(), v.GetPayloads()["application/x-lua"])
					//		}
					//	}
					//}
					//
					//// execute initially loaded code
					//err = LE.DoString(luaCode, tctx.ID)
					//if err != nil {
					//	return &api.EdgeCallResult{
					//		ID:            tctx.ID,
					//		Ok:            false,
					//		ResultPayload: strings.Join([]string{err.Error(), LE.GetPrints()}, "\n"),
					//	}, nil
					//}
					//
					//tres := LE.Get(-1)
					//// we have a function, need to call it
					//if tres.Type() == lua.LTFunction {
					//	// push the function to the lua stack
					//	LE.L.Push(tres)
					//	// function requires an argument to be passed (args)
					//	LE.L.Push(lengine.GoToLua(LE.L, args))
					//	// call the function with the argument
					//	err = LE.L.PCall(1, 1, nil)
					//	if err != nil {
					//		a.log.Error("failed to execute PCall", zap.Error(err))
					//		//return &api.EdgeCallResult{
					//		//	ID:            tctx.ID,
					//		//	Ok:            false,
					//		//	ResultPayload: strings.Join([]string{err.Error(), LE.GetPrints()}, "\n"),
					//		//}, nil
					//	}
					//}
					//
					//// todo: protect as well (Top)
					//result := lengine.ToGoAny(tres)
					//// we should not Pop values if there are no values on the Lua stack
					//if LE.L.GetTop() != 0 {
					//	LE.Pop(1)
					//}
					//
					//// Convert the result back to JSON
					//jsonResult, err := json.Marshal(result)
					//if err != nil {
					//	return &api.EdgeCallResult{
					//		ID:            tctx.ID,
					//		Ok:            false,
					//		ResultPayload: strings.Join([]string{fmt.Sprintf("failed to marshal result to JSON: %v", err), LE.GetPrints()}, "\n"),
					//	}, nil
					//}
					//
					//oper := &executorV1.UpdateDataOperation{
					//	BranchUuid:    tctx.ResultAddr.BranchUUID,
					//	NodeUuid:      tctx.ResultAddr.NodeUUID,
					//	Uuid:          tctx.ResultAddr.DataUUID,
					//	Version:       1,
					//	Content:       toPtr(string(jsonResult)),
					//	Discriminator: toPtr(tctx.ID),
					//}
					//
					//operAny, err := anypb.New(oper)
					//if err != nil {
					//	return nil, status.Error(codes.Internal, fmt.Sprintf("failed to marshal operation to anypb: %v", err))
					//}
					//
					//_, err = executorSrvV1.NewExecutorServiceClient(a.eecoreclient).Execute(metadata.NewOutgoingContext(ctx, metadata.Pairs("token", string(token))), &executorV1.ExecuteRequest{
					//	Operations: []*anypb.Any{operAny},
					//})
					//if err != nil {
					//	return nil, status.Error(codes.Internal, fmt.Sprintf("execute request failed: %v", err))
					//}
					//
					//return &api.EdgeCallResult{
					//	ID:            tctx.ID,
					//	Ok:            true,
					//	Result:        tctx.ResultAddr,
					//	ResultPayload: LE.GetPrints(),
					//}, nil

					e.Close()
				case "wasm":
				// TODO: implement wasm runtime
				default:
					r.log.Warn("unknown target", zap.String("target", target.Modules[i]))
				}

			}
		}

		select {
		case v.Response <- &api.TaskResult{
			ID:      v.ID,
			Error:   nil,
			Payload: []byte("Hello, RUNTIME!!!!!!!"),
		}:
			r.log.Debug("runtime: sent the response", zap.String("id", v.ID))
		default:
			r.log.Warn("runtime: unable to send the response, channel if full/nil", zap.String("id", v.ID))
		}
	}
}

func (r *Runtime) Stop(ctx context.Context) {}
