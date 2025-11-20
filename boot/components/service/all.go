package service

import "github.com/wippyai/runtime/api/boot"

func All() []boot.Component {
	return []boot.Component{
		Directory(),
		Embed(),
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
		OTel(),
		OTelHTTP(),
		OTelProcess(),
		OTelInterceptor(),
		OTelQueue(),
		InterceptorRetry(),
	}
}
