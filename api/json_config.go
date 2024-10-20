package api

// todo: isolate from api
// todo: this is actually system event types, per plugin event types must be in plugin
// todo: add transaction start and commit

type JSONConfiguration struct {
	Servers map[string]*Server `json:"servers"`
	Apps    map[string]*App    `json:"apps"`
}

type Server struct {
	ID      string `json:"id"`
	Type    string `json:"type"`
	Address string `json:"address"`
	TLS     *TLS   `json:"tls"`
}

type TLS struct{}

type App struct {
	ID           string `json:"id"`
	Type         string `json:"type"`
	TargetServer string `json:"target_server"`
	SourceCode   string `json:"source_code"`
	// lua: [http, env], wasm: [foo, bar]
	Extensions []string    `json:"extensions"`
	Paths      []string    `json:"paths"`
	Pipeline   []*Pipeline `json:"pipeline"`
}

type Pipeline struct {
	Name    string `json:"name"`
	Runtime string `json:"runtime"`
}
