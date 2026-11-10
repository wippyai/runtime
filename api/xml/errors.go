// SPDX-License-Identifier: MPL-2.0

package xml

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

// Errors returned by XML reader and writer operations.
var (
	ErrReaderClosed = apierror.WithDetails(apierror.Invalid, "xml reader is closed", attrs.Bag{"code": "state"})
	ErrWriterClosed = apierror.WithDetails(apierror.Invalid, "xml writer is closed", attrs.Bag{"code": "state"})
	ErrNoTable      = apierror.WithDetails(apierror.Internal, "resource table not available", attrs.Bag{"code": "io"})
	ErrNotFound     = apierror.WithDetails(apierror.Invalid, "xml reader or writer not found", attrs.Bag{"code": "state"})
	ErrNotSelected  = apierror.WithDetails(apierror.Invalid, "xml reader has no record selection", attrs.Bag{"code": "state"})
)
