package boot

const (
	// Core plugins - always enabled
	Core       = "core"
	AppContext = "appcontext"
	Logger     = "logger"
	EventBus   = "eventbus"
	PIDGen     = "pidgen"

	// Core infrastructure plugins
	Transcoder = "transcoder"
	LogManager = "logmanager"
	Security   = "security"
	Registry   = "registry"
	Supervisor = "supervisor"

	// System plugins
	Filesystem  = "filesystem"
	Environment = "env"
	Resources   = "resources"
	Interceptor = "interceptor"
	Functions   = "functions"
	Process     = "process"
	PubSub      = "pubsub"
	Topology    = "topology"
	Contracts   = "contracts"

	// Service plugins
	HTTP              = "http"
	SQL               = "sql"
	SQLStore          = "sqlstore"
	MemStore          = "memstore"
	TokenStore        = "tokenstore"
	Terminal          = "terminal"
	ProcessSupervisor = "process_supervisor"
	EphemeralHost     = "ephemeral_host"
	NativeExec        = "exec"
	Template          = "template"
	YAMLPolicy        = "policy"
	AWSConfig         = "aws_config"
	S3                = "s3"

	// Lua runtime
	LuaEngine = "lua_engine"

	// Lua modules
	LuaHTTP         = "lua_http"
	LuaSQL          = "lua_sql"
	LuaExec         = "lua_exec"
	LuaWebSocket    = "lua_websocket"
	LuaFS           = "lua_fs"
	LuaStore        = "lua_store"
	LuaProcess      = "lua_process"
	LuaFunc         = "lua_func"
	LuaRegistry     = "lua_registry"
	LuaSecurity     = "lua_security"
	LuaTemplate     = "lua_template"
	LuaCrypto       = "lua_crypto"
	LuaText         = "lua_text"
	LuaBtea         = "lua_btea"
	LuaExcel        = "lua_excel"
	LuaTreeSitter   = "lua_treesitter"
	LuaCloudStorage = "lua_cloudstorage"
	LuaContract     = "lua_contract"
	LuaOTel         = "lua_otel"
	LuaExpr         = "lua_expr"
	LuaHTML         = "lua_html"
	LuaSystem       = "lua_system"
)
