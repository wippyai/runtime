package transport

import "errors"

var (
	ErrNoHTTPRequest = errors.New("no HTTP request in context")
)
