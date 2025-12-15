package nil

import apierror "github.com/wippyai/runtime/api/error"

// Sentinel errors
var (
	ErrNoHeadVersion        = apierror.New(apierror.NotFound, "no head version set")
	ErrHistoryNotAvailable  = apierror.New(apierror.Unavailable, "version history not available: registry configured with history disabled (enable_history=false)")
	ErrRollbackNotSupported = apierror.New(apierror.Unavailable, "version rollback not supported: registry configured with history disabled (enable_history=false)")
)
