package cloudstorage

import "errors"

// Errors returned by cloud storage operations.
var (
	ErrObjectNotFound = errors.New("object not found")
	ErrAccessDenied   = errors.New("access denied")
	ErrBucketNotFound = errors.New("bucket not found")
)
