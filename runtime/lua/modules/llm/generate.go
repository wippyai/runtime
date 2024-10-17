package llm

import (
	"context"
	"strings"

	mlqsReqV1 "git.spiralscout.com/estimation-engine/api/gen/go/mlq/request/v1"
	"git.spiralscout.com/estimation-engine/go-lua"
	"github.com/google/uuid"
	"google.golang.org/grpc/metadata"
)

func (m *Module) generate(l *lua.LState) int {
	if l.GetTop() != 3 {
		l.Push(lua.LNil)
		l.Push(lua.LString("expected 3 arguments, system prompts array (strings), user prompts array (strings) and JSON with a model config"))
		return 2
	}

	contentSB := new(strings.Builder)
	defer contentSB.Reset()

	msgs := make([]*mlqsReqV1.Message, 0, 2)

	msgs = append(msgs, parseTablePrompt(l.CheckTable(1), "system", contentSB))
	msgs = append(msgs, parseTablePrompt(l.CheckTable(2), "user", contentSB))

	req := &mlqsReqV1.PushRequest{
		Uuid: uuid.NewString(),
		Model: &mlqsReqV1.Model{
			ModelOptions: &mlqsReqV1.ModelOptions{},
		},
		Prompt: msgs,
		Options: &mlqsReqV1.Options{
			PromptSize: toPtr(int64(0)),
		},
		Scope: make(map[string]string, 1),
	}

	req.Scope["authToken"] = m.token

	optionsTable := l.CheckTable(3)
	modelv := l.GetField(optionsTable, "model")
	maxTokens := l.GetField(optionsTable, "max_tokens")
	temperature := l.GetField(optionsTable, "temperature")

	if modelv == lua.LNil {
		l.Push(lua.LNil)
		l.Push(lua.LString("model is required"))
		return 2
	}

	if maxTokens == lua.LNil {
		l.Push(lua.LNil)
		l.Push(lua.LString("max_tokens is required"))
		return 2
	}

	if temperature == lua.LNil {
		temperature = lua.LNumber(0.5)
	}

	// parse model and provider
	req.Model.Name, req.Provider = parseModelNameReverse(modelv.String())
	req.Options.MaxTokens = int64(maxTokens.(lua.LNumber))
	req.Model.ModelOptions.Temperature = float32(temperature.(lua.LNumber))

	gresp, err := m.service.GenerateAIResponse(metadata.NewOutgoingContext(context.Background(), metadata.Pairs("token", m.token)), req)
	if err != nil {
		return 0
	}

	l.Push(lua.LString(gresp.Content))
	l.Push(lua.LNil)

	return 2
}
