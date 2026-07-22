package api

import (
	"context"
	"fmt"
	"strings"

	"jarvis/internal/sharedmem"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/protocol/consts"
)

// sharedMemoryUpdatedBy 标记后台人工编辑的来源，与 Agent 自动更新区分。
const sharedMemoryUpdatedBy = "user"

// sharedMemoryReadWriter 是共享记忆 handler 依赖的最小读写接口，
// *sharedmem.SharedMemoryService 实现它，测试可打桩。
type sharedMemoryReadWriter interface {
	Get(ctx context.Context) (*sharedmem.SharedMemoryView, error)
	Upsert(ctx context.Context, content, updatedBy string) (*sharedmem.SharedMemoryView, error)
}

// GetSharedMemory 返回当前共享记忆视图；无行时返回空内容的可用视图（不是 404）。
func GetSharedMemory(svc sharedMemoryReadWriter) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		view, err := svc.Get(ctx)
		if err != nil {
			writeAPIError(c, consts.StatusInternalServerError, 50070, fmt.Errorf("get shared memory failed: %s", strings.TrimSpace(err.Error())))
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": view})
	}
}

// UpdateSharedMemory 整段覆盖保存共享记忆。允许空 content（可清空），但请求体格式
// 非法要 fail-fast 400。
func UpdateSharedMemory(svc sharedMemoryReadWriter) app.HandlerFunc {
	return func(ctx context.Context, c *app.RequestContext) {
		var in struct {
			Content string `json:"content"`
		}
		if err := decodeStrictJSON(c.Request.Body(), &in); err != nil {
			writeAPIError(c, consts.StatusBadRequest, 40070, err)
			return
		}
		view, err := svc.Upsert(ctx, in.Content, sharedMemoryUpdatedBy)
		if err != nil {
			writeAPIError(c, consts.StatusInternalServerError, 50070, fmt.Errorf("save shared memory failed: %s", strings.TrimSpace(err.Error())))
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": view})
	}
}
