package httphandler

import (
	"errors"

	"github.com/ponyruntime/go-lua"
)

func checkUserData(ud *lua.LUserData) (*httpHandler, error) {
	if ud == nil {
		return nil, errors.New("expected userdata for http handler")
	}

	httph, ok := ud.Value.(*httpHandler)
	if !ok {
		return nil, errors.New("invalid userdata type for http handler")
	}

	if httph == nil {
		return nil, errors.New("http handler is nil")
	}

	if httph.r == nil {
		return nil, errors.New("http request is nil")
	}

	return httph, nil
}
