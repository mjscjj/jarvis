package api

import (
	"context"
	"fmt"
	"strings"

	"jarvis/internal/sharedmem"

	"code.byted.org/middleware/hertz/pkg/app"
	"code.byted.org/middleware/hertz/pkg/protocol/consts"
)

// sharedMemoryReadWriter 是共享记忆 handler 依赖的最小读写接口，
// *sharedmem.SharedMemoryService 实现它，测试可打桩。
type sharedMemoryReadWriter interface {
	Get(ctx context.Context) (*sharedmem.SharedMemoryView, error)
	Upsert(ctx context.Context, content string) (*sharedmem.SharedMemoryView, error)
}

// GetSharedMemory 返回本机 Markdown 中的当前共享记忆。
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
		view, err := svc.Upsert(ctx, in.Content)
		if err != nil {
			writeAPIError(c, consts.StatusInternalServerError, 50070, fmt.Errorf("save shared memory failed: %s", strings.TrimSpace(err.Error())))
			return
		}
		c.JSON(consts.StatusOK, map[string]any{"code": 0, "data": view})
	}
}
