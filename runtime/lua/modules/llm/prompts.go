package llm

import (
	"strings"

	mlqsReqV1 "git.spiralscout.com/estimation-engine/api/gen/go/mlq/request/v1"
	"git.spiralscout.com/estimation-engine/go-lua"
)

func parseTablePrompt(prompts *lua.LTable, role string, buf *strings.Builder) *mlqsReqV1.Message {
	// second arg - system prompts
	if prompts != nil {
		content := make([]*mlqsReqV1.Content, 0, 2)
		prompts.ForEach(func(_, value lua.LValue) {
			if str, ok := value.(lua.LString); ok {
				content = append(content, &mlqsReqV1.Content{
					Type:    mlqsReqV1.MessageType_MESSAGE_TYPE_TEXT,
					Content: str.String(),
				})

				buf.WriteString(str.String())
			}
		})

		return &mlqsReqV1.Message{
			Role:    role,
			Content: content,
		}
	}

	return nil
}
