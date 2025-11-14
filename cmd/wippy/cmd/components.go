package cmd

import (
	"github.com/ponyruntime/pony/api/boot"
	"github.com/ponyruntime/pony/boot/components/core/core"
	lua "github.com/ponyruntime/pony/boot/components/runtime/runtime/lua"
	"github.com/ponyruntime/pony/boot/components/service/service"
	"github.com/ponyruntime/pony/boot/components/system/system"
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
