// SPDX-License-Identifier: MPL-2.0

package metrics

import "github.com/wippyai/runtime/api/boot"

// Name is the component name for metrics.
const Name boot.Name = "metrics"

// InterceptorName is the component name for the function-call metrics interceptor.
const InterceptorName boot.Name = "metrics-interceptor"

// ProcessName is the component name for the process lifecycle metrics handler.
const ProcessName boot.Name = "metrics-process"

// interceptorName is the external dependency name of the function interceptor
// registry component (boot/components/system.Interceptor).
const interceptorName boot.Name = "interceptor"

// lifecycleName is the external dependency name of the process lifecycle
// registry component (boot/components/system.LifecycleName).
const lifecycleName boot.Name = "system.lifecycle"
