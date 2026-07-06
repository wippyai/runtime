// SPDX-License-Identifier: MPL-2.0

package plan

import apierror "github.com/wippyai/runtime/api/error"

// NewEmptyDeployError reports a deploy without specs.
func NewEmptyDeployError(deployID string) apierror.Error {
	return apierror.New(apierror.Invalid, "deploy has no specs: "+deployID).WithRetryable(apierror.False)
}

// NewInvalidSpecError reports a declaration validation failure.
func NewInvalidSpecError(detail string) apierror.Error {
	return apierror.New(apierror.Invalid, "invalid spec: "+detail).WithRetryable(apierror.False)
}

// NewDependencyCycleError reports a cycle among hard dependency edges,
// naming the strongly connected components (design §8.2).
func NewDependencyCycleError(detail string) apierror.Error {
	return apierror.New(apierror.Invalid, "dependency_cycle: "+detail).WithRetryable(apierror.False)
}
