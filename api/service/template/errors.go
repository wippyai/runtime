package template

import (
	"errors"

	apierror "github.com/wippyai/runtime/api/error"
)

// Sentinel errors for template operations.
var (
	ErrTemplateNotFound = errors.New("template not found")
	ErrSetNotFound      = errors.New("template set not found")
	ErrSetNotEmpty      = errors.New("template set is not empty")
)

var (
	ErrEmptySource            = apierror.New(apierror.Invalid, "source is required").WithRetryable(apierror.False)
	ErrEmptySetName           = apierror.New(apierror.Invalid, "set name is required").WithRetryable(apierror.False)
	ErrEmptyDelimiters        = apierror.New(apierror.Invalid, "delimiters cannot be empty").WithRetryable(apierror.False)
	ErrEmptyCommentDelimiters = apierror.New(apierror.Invalid, "comment delimiters cannot be empty").WithRetryable(apierror.False)
	ErrConflictingDelimiters  = apierror.New(apierror.Invalid, "delimiters and comment delimiters cannot be the same").WithRetryable(apierror.False)
	ErrEmptyExtensions        = apierror.New(apierror.Invalid, "at least one file extension is required").WithRetryable(apierror.False)
)
