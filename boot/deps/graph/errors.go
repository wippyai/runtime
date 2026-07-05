// SPDX-License-Identifier: MPL-2.0

package graph

import (
	"github.com/wippyai/runtime/api/attrs"
	apierror "github.com/wippyai/runtime/api/error"
)

func NewInvalidModuleNameError(name string) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid module name format: "+name+" (expected org/module)").
		WithDetails(attrs.NewBagFrom(map[string]any{"name": name}))
}

func NewEmptyModuleNameError(name string) apierror.Error {
	return apierror.New(apierror.Invalid, "empty organization or module name: "+name).
		WithDetails(attrs.NewBagFrom(map[string]any{"name": name}))
}
