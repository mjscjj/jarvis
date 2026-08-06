package api

import (
	"context"
	"encoding/json"

	"jarvis/internal/contextsnap"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

type ContextAssembler interface {
	AssembleConversation(context.Context, contextsnap.AssembleOptions) (json.RawMessage, error)
}

type assembleContextRequest struct {
	ChatID         string          `json:"chat_id"`
	ProjectID      *uint64         `json:"project_id"`
	RequestContext json.RawMessage `json:"request_context"`
}

// AssembleContext exposes the one canonical background assembler to callers
// such as CC Connect. It is read-only: callers identify the conversation or
// project, while Jarvis owns all authoritative background lookup.
func AssembleContext(assembler ContextAssembler) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var request assembleContextRequest
		if err := decodeStrictJSON(c.Request.Body(), &request); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40080, err)
			return
		}
		snapshot, err := assembler.AssembleConversation(ctx, contextsnap.AssembleOptions{
			ChatID: request.ChatID, ProjectID: request.ProjectID, RequestContext: request.RequestContext,
		})
		if err != nil {
			writeAPIError(c, consts.StatusInternalServerError, 50080, err)
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": snapshot})
	}
}
