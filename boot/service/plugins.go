//go:build !plugin_minimal

package service

import "github.com/ponyruntime/pony/api/boot"

func All() []boot.Plugin {
	return []boot.Plugin{
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
		InterceptorManager(),
		InterceptorOtel(),
		InterceptorRetry(),
	}
}
