package cmd

import (
	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/boot/components/core/core"
	lua "github.com/wippyai/runtime/boot/components/runtime/runtime/lua"
	"github.com/wippyai/runtime/boot/components/service/service"
	"github.com/wippyai/runtime/boot/components/system/system"
)

// StandardComponents returns the default component set for wippy runtime.
// Applications can customize by modifying this list or creating their own.
func StandardComponents() []boot.Component {
	components := []boot.Component{}
	components = append(components, core.All()...)
	components = append(components, system.All()...)
	components = append(components, service.All()...)
	components = append(components, lua.Engine())
	return components
}
