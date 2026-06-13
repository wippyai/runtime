// SPDX-License-Identifier: MPL-2.0

package lua

import "github.com/wippyai/runtime/api/boot"

func All() []boot.Component {
	return []boot.Component{
		Engine(),
		Base64(),
		CloudStorage(),
		Compress(),
		Contract(),
		Ctx(),
		Crypto(),
		Env(),
		Eval(),
		EvalRunner(),
		Events(),
		Excel(),
		Exec(),
		Expr(),
		FS(),
		Funcs(),
		HTML(),
		HTTP(),
		Hub(),
		IO(),
		JSON(),
		Logger(),
		Metrics(),
		Payload(),
		Queue(),
		Registry(),
		Security(),
		SQL(),
		Store(),
		Stream(),
		System(),
		Template(),
		Text(),
		Time(),
		TreeSitter(),
		TTY(),
		UUID(),
		WebSocket(),
		Workflow(),
		YAML(),
		LSP(),
		PG(),
		CDC(),
	}
}
