package auth

import "errors"

var (
	ErrNotAuthenticated  = errors.New("not authenticated")
	ErrTokenExpired      = errors.New("token expired")
	ErrTokenInvalid      = errors.New("invalid token")
	ErrInsufficientScope = errors.New("insufficient scope")
	ErrOrgAccessDenied   = errors.New("organization access denied")
)
