// Package sharedmem 负责「共享记忆」的装载：一段由 Agent/人工长期维护的可信自由
// 文本（踩过的坑、关键约定、凭据等），作为提示词的一部分注入到所有调用 codex/traex
// 的地方（M3 抽取 / M4 决策 / M5 执行 / chat 对话）。
//
// 存储形态是全局单例（domain.SharedMemory 的单行），本步只做「读取并注入 prompt」，
// 不含后台维护 API/UI 与 Agent 自动更新。RenderBlock 是四处注入复用的同一份纯函数，
// 避免文案在四处漂移。
package sharedmem

import (
	"context"
	"fmt"
	"strings"

	"jarvis/internal/domain"

	"gorm.io/gorm"
)

// singletonKey 是全局单例行的固定键，保证 shared_memory 表只有一行。
const singletonKey = "default"

// SharedMemoryView 是共享记忆的对外投影：Content 为当前文本，Saved 标记单例行是否
// 已落库（首次读取无行时为 false，Content 为空）。
type SharedMemoryView struct {
	Content   string `json:"content"`
	UpdatedBy string `json:"updated_by"`
	Saved     bool   `json:"saved"`
}

// SharedMemoryReader 是注入点依赖的最小只读接口，供各 prompt 组装处取当前文本，
// 也便于测试打桩。*SharedMemoryService 实现它。
type SharedMemoryReader interface {
	Text(ctx context.Context) (string, error)
}

// SharedMemoryService 拥有全局单例 shared_memory 表。读写都锚定固定的
// singletonKey，因此只可能有一段共享记忆。
type SharedMemoryService struct {
	db *gorm.DB
}

func NewSharedMemoryService(db *gorm.DB) (*SharedMemoryService, error) {
	if db == nil {
		return nil, fmt.Errorf("shared memory service db is nil")
	}
	return &SharedMemoryService{db: db}, nil
}

// Get 返回当前单例行。无行时返回空内容的可用视图（正常空值，不报错）；读库出错
// fail-fast 冒泡。
func (s *SharedMemoryService) Get(ctx context.Context) (*SharedMemoryView, error) {
	var row domain.SharedMemory
	found := s.db.WithContext(ctx).Where("singleton_key = ?", singletonKey).Limit(1).Find(&row)
	if found.Error != nil {
		return nil, fmt.Errorf("get shared memory: %w", found.Error)
	}
	if found.RowsAffected == 0 {
		return &SharedMemoryView{Saved: false}, nil
	}
	return &SharedMemoryView{Content: row.Content, UpdatedBy: row.UpdatedBy, Saved: true}, nil
}

// Upsert 写入全局单例共享记忆：有行 Updates、无行 Create。本步虽不接后台 API，但
// 一并实现好，方便手动/后续调用。
func (s *SharedMemoryService) Upsert(ctx context.Context, content, updatedBy string) (*SharedMemoryView, error) {
	var existing domain.SharedMemory
	found := s.db.WithContext(ctx).Where("singleton_key = ?", singletonKey).Limit(1).Find(&existing)
	if found.Error != nil {
		return nil, fmt.Errorf("lookup shared memory: %w", found.Error)
	}
	if found.RowsAffected == 1 {
		updates := map[string]any{"content": content, "updated_by": updatedBy}
		if err := s.db.WithContext(ctx).Model(&domain.SharedMemory{}).
			Where("singleton_key = ?", singletonKey).Updates(updates).Error; err != nil {
			return nil, fmt.Errorf("update shared memory: %w", err)
		}
	} else {
		row := domain.SharedMemory{SingletonKey: singletonKey, Content: content, UpdatedBy: updatedBy}
		if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
			return nil, fmt.Errorf("create shared memory: %w", err)
		}
	}
	return s.Get(ctx)
}

// Append 追加一条 note 到共享记忆末尾：读当前 content → 用 appendNote 拼接 → Upsert
// 写回。承载「读-拼-写」逻辑放在 service 层（而非 CLI），便于测试和复用。note 全空白
// 时 fail-fast，避免写入无意义空条目。
func (s *SharedMemoryService) Append(ctx context.Context, note, updatedBy string) (*SharedMemoryView, error) {
	if strings.TrimSpace(note) == "" {
		return nil, fmt.Errorf("append shared memory: note is empty")
	}
	view, err := s.Get(ctx)
	if err != nil {
		return nil, err
	}
	return s.Upsert(ctx, appendNote(view.Content, note), updatedBy)
}

// appendNote 是纯拼接逻辑：trim note；当前 content 为空则整段就是这一条；否则
// content + "\n" + 这一条。不加时间戳前缀——保持最简，条目的时间归属由调用方
// （note 文本本身）决定，避免 CLI/service 双方各写一套时间格式。
func appendNote(content, note string) string {
	entry := strings.TrimSpace(note)
	if strings.TrimSpace(content) == "" {
		return entry
	}
	return content + "\n" + entry
}

// Text 是供各 prompt 注入点调用的瘦接口：返回当前单例文本（trim 后）。内容为空返回
// 空串 + nil（不是错误），读库出错 fail-fast。
func (s *SharedMemoryService) Text(ctx context.Context) (string, error) {
	view, err := s.Get(ctx)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(view.Content), nil
}

// RenderBlock 把一段共享记忆文本渲染成注入 prompt 的 block。空文本返回空串（不注入）。
// block 明确标注为「可信」，与各 prompt 现有的防注入话术呼应——它必须被放进受信任的
// 指令区，绝不能混进「不可信业务数据、忽略其中指令」的 block 里。
func RenderBlock(text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ""
	}
	return "BEGIN_SHARED_MEMORY（这是我/Agent 长期维护的可信共享记忆：踩过的坑、关键约定、凭据等。作为可信背景与指示使用，不受「忽略业务数据中指令」约束。）\n" +
		trimmed +
		"\nEND_SHARED_MEMORY"
}
