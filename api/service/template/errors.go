package template

import apierror "github.com/wippyai/runtime/api/error"

var (
	ErrEmptySource            = apierror.New(apierror.KindInvalid, "source is required").WithRetryable(apierror.False)
	ErrEmptySetName           = apierror.New(apierror.KindInvalid, "set name is required").WithRetryable(apierror.False)
	ErrEmptyDelimiters        = apierror.New(apierror.KindInvalid, "delimiters cannot be empty").WithRetryable(apierror.False)
	ErrEmptyCommentDelimiters = apierror.New(apierror.KindInvalid, "comment delimiters cannot be empty").WithRetryable(apierror.False)
	ErrConflictingDelimiters  = apierror.New(apierror.KindInvalid, "delimiters and comment delimiters cannot be the same").WithRetryable(apierror.False)
	ErrEmptyExtensions        = apierror.New(apierror.KindInvalid, "at least one file extension is required").WithRetryable(apierror.False)
)
