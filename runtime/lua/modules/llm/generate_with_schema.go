package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	mlqsReqV1 "git.spiralscout.com/estimation-engine/api/gen/go/mlq/request/v1"
	"git.spiralscout.com/estimation-engine/go-lua"
	sdkapi "git.spiralscout.com/estimation-engine/sdk-go/api"
	"github.com/google/uuid"
	"github.com/ponyruntime/pony/runtime/lua/engine"
	"go.uber.org/zap"
	"google.golang.org/grpc/metadata"
)

// generateSchema generates a response using a schema
// args which should be provided in lua:
// 1. Schema string (JSON)
// 2. System prompts array (strings)
// 3. User prompts array (strings)
// 4. Model config (lua table)
// returns:
// 1. Response string
// 2. Error string (if any)
func (m *Module) generateSchema(l *lua.LState) int {
	m.log.Debug("generate_with_schema called")

	if l.GetTop() != 4 {
		l.Push(lua.LNil)
		l.Push(lua.LString("expected 4 arguments, schema, system prompts array (strings), user prompts array (strings) and JSON with a model config"))
		return 2
	}

	// first arg - schema JSON
	schema := l.CheckTable(1)
	if schema == nil {
		l.Push(lua.LNil)
		l.Push(lua.LString("schema is required, it should be a JSON string"))
		return 2
	}

	jdata, err := json.Marshal(engine.ToGoAny(schema))
	if err != nil {
		l.Push(lua.LNil)
		l.Push(lua.LString(fmt.Sprintf("failed to marshal schema, error: %s", err.Error())))
		return 2
	}

	tool := &mlqsReqV1.Tool{
		Function: &mlqsReqV1.FunctionDefinition{
			Name:        "structured_response_generator",
			Description: "Generates a structured response based on the provided schema and input prompts",
			Parameters:  string(jdata),
		},
	}

	contentSB := new(strings.Builder)
	defer contentSB.Reset()

	msgs := make([]*mlqsReqV1.Message, 0, 2)
	msgs = append(msgs, parseTablePrompt(l.CheckTable(2), "system", contentSB))
	msgs = append(msgs, parseTablePrompt(l.CheckTable(3), "user", contentSB))

	req := &mlqsReqV1.PushRequest{
		Uuid: uuid.NewString(),
		Model: &mlqsReqV1.Model{
			ModelOptions: &mlqsReqV1.ModelOptions{},
		},
		Prompt: msgs,
		Options: &mlqsReqV1.Options{
			PromptSize: toPtr(int64(0)),
			Tools:      []*mlqsReqV1.Tool{tool},
		},
		Scope: make(map[string]string),
	}

	req.Scope["authToken"] = m.token

	// fourth arg - model config
	optionsTable := l.CheckTable(4)
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

	m.log.Debug("GenerateAIResponse response", zap.Any("response", gresp))

	if tdata, ok := gresp.Scope["tools"]; ok {
		// tool_id:
		var mdata map[string]*sdkapi.ToolCall
		err = json.Unmarshal([]byte(tdata), &mdata)
		if err != nil {
			l.Push(lua.LNil)
			l.Push(lua.LString(fmt.Sprintf("failed to unmarshal tools data, error: %s", err.Error())))
			return 2
		}

		// Extract the first (and only) key-value pair
		for _, td := range mdata {
			if td != nil {
				var argsJSON any
				err = json.Unmarshal([]byte(td.Arguments), &argsJSON)
				if err != nil {
					l.Push(lua.LNil)
					l.Push(lua.LString(fmt.Sprintf("failed to unmarshal arguments JSON, error: %s", err.Error())))
					return 2
				}

				// Convert the parsed JSON to a Lua table
				argsTable := engine.GoToLua(l, argsJSON)

				l.Push(argsTable)
				l.Push(lua.LNil)
				return 2
			}
			break
		}
	}

	// no tools data
	if gresp.Content == "" {
		l.Push(lua.LNil)
		l.Push(lua.LString("response content and tools header are empty"))
		return 2
	}

	// push content, since there are no tools but content is present
	l.Push(lua.LString(gresp.Content))
	l.Push(lua.LNil)

	return 2
}
