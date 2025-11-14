//go:build plugin_lua

package lua

import "github.com/ponyruntime/pony/api/boot"

func All() []boot.Component {
	return []boot.Component{
		Engine(),
		LuaBase64(),
		LuaBTEA(),
		LuaChannel(),
		LuaCloudStorage(),
		LuaContract(),
		LuaCrypto(),
		LuaEvents(),
		LuaExcel(),
		LuaExec(),
		LuaExpr(),
		LuaFS(),
		LuaFunc(),
		LuaHTML(),
		LuaHTTP(),
		LuaJSON(),
		LuaOTel(),
		LuaPayload(),
		LuaProcess(),
		LuaRegistry(),
		LuaSecurity(),
		LuaSQL(),
		LuaStore(),
		LuaSubscribe(),
		LuaSystem(),
		LuaTemplate(),
		LuaText(),
		LuaTime(),
		LuaTreeSitter(),
		LuaUpstream(),
		LuaUUID(),
		LuaWebSocket(),
		LuaYAML(),
	}
}
