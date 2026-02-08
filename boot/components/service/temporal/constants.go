// Package temporal provides boot components that wire up Temporal clients,
// workers, activity listeners, and workflow listeners into the application.
package temporal

import "github.com/wippyai/runtime/api/boot"

const (
	Name              boot.Name = "temporal"
	InterceptorName   boot.Name = "temporal.interceptor"
	DataConverterName boot.Name = "temporal.dataconverter"
)
