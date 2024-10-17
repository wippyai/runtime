package llm

import (
	providersV1 "git.spiralscout.com/estimation-engine/api/gen/go/common/aiproviders/v1"
	modelsV1 "git.spiralscout.com/estimation-engine/api/gen/go/common/models/v1"
	mlqsV1 "git.spiralscout.com/estimation-engine/api/gen/go/mlq/service/v1"
	"git.spiralscout.com/estimation-engine/go-lua"
	"go.uber.org/zap"
	"google.golang.org/grpc"
)

type Module struct {
	service mlqsV1.MLQServiceClient
	log     *zap.Logger
	token   string
}

func NewLLMModule(gclient *grpc.ClientConn, token string, lg *zap.Logger) *Module {
	return &Module{
		service: mlqsV1.NewMLQServiceClient(gclient),
		log:     lg,
		token:   token,
	}
}

// Loader is the module loader function.
func (m *Module) Loader(l *lua.LState) int {
	t := l.NewTable()

	lapi := map[string]lua.LGFunction{
		"generate":             m.generate,
		"generate_with_schema": m.generateSchema,
	}

	l.SetFuncs(t, lapi)
	l.Push(t)
	return 1
}

// reverse of parseModelName, accepts a model name and returns the corresponding modelsV1.Model
func parseModelNameReverse(name string) (modelsV1.Model, providersV1.Provider) {
	switch name {
	case "gpt-4o":
		return modelsV1.Model_MODEL_GPT4_O_MINI, providersV1.Provider_PROVIDER_OPENAI
	case "gpt-4-turbo":
		return modelsV1.Model_MODEL_GPT4_TURBO, providersV1.Provider_PROVIDER_OPENAI
	case "gpt-3.5-turbo":
		return modelsV1.Model_MODEL_GTP35_TURBO, providersV1.Provider_PROVIDER_OPENAI
	case "gpt-4o-mini":
		return modelsV1.Model_MODEL_GPT4_O_MINI, providersV1.Provider_PROVIDER_OPENAI
	case "chatgpt-4o-latest":
		return modelsV1.Model_MODEL_GPT4_O_LATEST, providersV1.Provider_PROVIDER_OPENAI
	case "claude-3-5-sonnet-2024062", "claude-3-5-sonnet":
		return modelsV1.Model_MODEL_CLAUDE35_SONNET_2024_06_20, providersV1.Provider_PROVIDER_CLAUDE
	case "o1-mini":
		return modelsV1.Model_MODEL_OPENAI_O1_MINI_LATEST, providersV1.Provider_PROVIDER_OPENAI
	case "o1":
		return modelsV1.Model_MODEL_OPENAI_O1_LATEST, providersV1.Provider_PROVIDER_OPENAI
	case "gemini-1.5-pro":
		return modelsV1.Model_MODEL_GEMINI_1_5_PRO, providersV1.Provider_PROVIDER_GEMINI
	case "gemini-1.5-pro-flash":
		return modelsV1.Model_MODEL_GEMINI_1_5_FLASH, providersV1.Provider_PROVIDER_GEMINI
	case "gemini-1.0-pro":
		return modelsV1.Model_MODEL_GEMINI_1_0_PRO, providersV1.Provider_PROVIDER_GEMINI
	case "gemini-1.0-pro-vision":
		return modelsV1.Model_MODEL_GEMINI_1_0_PRO_VISION, providersV1.Provider_PROVIDER_GEMINI

	default:
		panic("unknown model name")
	}
}

func toPtr[T any](val T) *T {
	return &val
}
