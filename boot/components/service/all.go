package service

import "github.com/wippyai/runtime/api/boot"

func All() []boot.Component {
	return []boot.Component{
		Directory(),
		MemStore(),
		SQLStore(),
		Template(),
		Terminal(),
		Exec(),
		Host(),
		Policy(),
		Contract(),
		EnvService(),
		S3(),
		AWS(),
		SQL(),
		ProcessFunc(),
		TokenStore(),
		ProcessSupervisor(),
		HTTP(),
		InterceptorDebug(),
		InterceptorOtel(),
		InterceptorRetry(),
	}
}
