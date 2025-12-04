package lua

import "github.com/wippyai/runtime/api/boot"

func All() []boot.Component {
	return []boot.Component{
		Engine(),
		Base64(),
		Compress(),
		Ctx(),
		Crypto(),
		Env(),
		Events(),
		Excel(),
		Exec(),
		Expr(),
		FS(),
		Funcs(),
		HTML(),
		HTTP(),
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
		UUID(),
		WebSocket(),
		YAML(),
	}
}
