package handler

import (
	"github.com/ponyruntime/pony/api/context"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/runtime"
	config "github.com/ponyruntime/pony/api/service/http"
	"net/http"
)

type EndpointHandler struct {
	exec runtime.Executor
	dtt  payload.Transcoder
}

func NewEndpointHandler(exec runtime.Executor, dtt payload.Transcoder) *EndpointHandler {
	return &EndpointHandler{exec: exec, dtt: dtt}
}

func (h *EndpointHandler) Handle(w http.ResponseWriter, r *http.Request) {
	v, ok := r.Context().Value(context.RouteInfoCtx).(*config.RouteInfo)
	if !ok {
		http.Error(w, "route not found", http.StatusInternalServerError)
		return
	}

	resultCh, err := h.exec.Execute(h.makeTask(r, v))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var result *runtime.Result
	select {
	case result = <-resultCh:
	case <-r.Context().Done():
		http.Error(w, "request cancelled", http.StatusRequestTimeout)
		return
	}

	h.writeResult(w, result)
}

func (h *EndpointHandler) makeTask(r *http.Request, info *config.RouteInfo) runtime.Task {
	return runtime.Task{
		Context: r.Context(),
		Target:  registry.ID(info.EndpointID),
		Payload: nil,
	}
}

func (h *EndpointHandler) writeResult(w http.ResponseWriter, result *runtime.Result) {
	if result.Error != nil {
		// todo: properly hide error message
		http.Error(w, result.Error.Error(), http.StatusInternalServerError)
		return
	}

	// todo: in future we can look for status code in the payload

	// example of transcoding to json
	out, err := h.dtt.Transcode(result.Payload, payload.Json)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	_, err = w.Write(out.Data().([]byte))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}
