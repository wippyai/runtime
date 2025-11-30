package lua

import "github.com/wippyai/runtime/api/boot"

func All() []boot.Component {
	return []boot.Component{
		Engine(),
		Base64(),
		Compress(),
		Ctx(),
		Crypto(),
		Events(),
		Excel(),
		Exec(),
		Expr(),
		FS(),
		Funcs(),
		HTML(),
		HTTP(),
		JSON(),
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
		UUID(),
		WebSocket(),
		YAML(),
	}
}
