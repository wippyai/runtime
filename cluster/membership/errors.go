package membership

import (
	apierror "github.com/wippyai/runtime/api/error"
)

var (
	ErrNoSecretKeyProvided = apierror.New(apierror.Invalid, "no secret key provided").WithRetryable(apierror.False)
)

func NewLoadSecretKeyError(err error) apierror.Error {
	return apierror.New(apierror.Invalid, "failed to load cluster secret key").WithCause(err).WithRetryable(apierror.False)
}

func NewCreateMemberlistError(err error) apierror.Error {
	return apierror.New(apierror.Internal, "failed to create memberlist").WithCause(err).WithRetryable(apierror.False)
}

func NewJoinClusterError(err error) apierror.Error {
	return apierror.New(apierror.Unavailable, "failed to join cluster").WithCause(err).WithRetryable(apierror.True)
}

func NewReadSecretFileError(err error) apierror.Error {
	return apierror.New(apierror.Invalid, "failed to read secret file").WithCause(err).WithRetryable(apierror.False)
}
