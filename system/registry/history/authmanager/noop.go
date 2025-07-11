package authmanager

import "net/http"

type Noop struct{}

func (Noop) SetAuth(http.Header) {}
