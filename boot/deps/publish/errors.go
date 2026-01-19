package publish

import "errors"

var (
	ErrNotAuthenticated  = errors.New("not authenticated")
	ErrVersionExists     = errors.New("version already exists")
	ErrInvalidVersion    = errors.New("invalid version format")
	ErrOrgAccessDenied   = errors.New("organization access denied")
	ErrModuleNotFound    = errors.New("module not found")
	ErrDigestMismatch    = errors.New("digest mismatch")
	ErrUploadExpired     = errors.New("upload URL expired")
	ErrPublishInProgress = errors.New("publish already in progress")
)
