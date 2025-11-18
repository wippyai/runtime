package lua

import "github.com/wippyai/runtime/api/boot"

func All() []boot.Component {
	return []boot.Component{
		Engine(),
		Base64(),
		BTEA(),
		Channel(),
		CloudStorage(),
		Compress(),
		Contract(),
		Context(),
		Crypto(),
		Events(),
		Excel(),
		Exec(),
		Expr(),
		FS(),
		Func(),
		HTML(),
		HTTP(),
		JSON(),
		OTel(),
		Payload(),
		Process(),
		Registry(),
		Security(),
		SQL(),
		Store(),
		Stream(),
		Subscribe(),
		System(),
		Template(),
		Text(),
		Time(),
		TreeSitter(),
		Upstream(),
		UUID(),
		WebSocket(),
		YAML(),
	}
}
