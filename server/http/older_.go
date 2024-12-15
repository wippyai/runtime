package http

//
//type Endpoint interface {
//	Configure(cfg *api.JSONConfiguration)
//	ServeHTTP(w http.ResponseWriter, r *http.Request)
//	start()
//	Stop(ctx context.Context)
//}

//
//// Configure uses JSONConfiguration to configure the endpoint
//func (e *Server) Configure(cfg *api.JSONConfiguration) {
//	e.log.Info("http: configuring the endpoint")
//
//	for name, v := range cfg.Servers {
//		e.log.Info("http: configuring server", zap.String("name", name), zap.String("type", v.Type), zap.String("address", v.Address))
//		switch v.Type {
//		// we're particularly interested in the HTTP endpoint
//		case "http":
//			e.server.Addr = v.Address
//		default:
//			e.log.Warn("http: skipping other endpoint types", zap.String("type", v.Type))
//		}
//	}
//
//	// TODO: pipeline should be attached here, ServeHTTP is the last step
//	e.server.Handler = e
//}
//
//func (e *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
//	e.log.Info("http: request received", zap.String("path", r.URL.Path))
//	// TODO: futures with proper:
//	// 1. Error handling
//	// 2. Timeouts and retries
//	// 3. Interceptors/additional callbacks support (e.g. logging)
//
//	// lock
//	// switch / map -> switch routes -> check id + src from config
//	// query args
//	//
//
//	// todo: rewrite and encapsulate into pipeline
//
//	// TODO: we should parse a body here: body + query
//	data, err := io.ReadAll(r.Body)
//	if err != nil {
//		// WARN: proper error handling
//		panic(err)
//	}
//
//	// TODO: get the args from query
//	q := r.URL.Query()
//
//	// from BODY: todo: read on demand inside endpoint pipeline
//	task := &api.Task{
//		App: "my-app-1",
//		// args, todo: redo
//		Payload: data,
//		Query:   q.Encode(), // todo: remove from here
//	}
//
//	// todo: inside pipeline
//	fut := e.exec.Await(context.Background(), task)
//
//	select {
//	case res := <-fut:
//		e.log.Info("http: task has been processed", zap.String("path", r.URL.Path))
//		if res.Error != nil {
//			e.log.Error("http: error processing the request", zap.Error(res.Error))
//			w.WriteHeader(http.StatusInternalServerError)
//			_, _ = w.Write([]byte("Internal component Error"))
//			return
//		}
//
//		w.WriteHeader(http.StatusOK)
//		_, _ = w.Write(res.Payload)
//	}
//}
//
//// start starts the HTTP server
//func (e *Server) start() {
//	e.log.Info("http: starting the server", zap.String("address", e.server.Addr))
//	go func() {
//		err := e.server.ListenAndServe()
//		if err != nil {
//			if errors.Is(err, http.ErrServerClosed) {
//				e.log.Info("http: server has been closed")
//			} else {
//				e.log.Error("http: server has stopped with error", zap.Error(err))
//			}
//		}
//	}()
//}
